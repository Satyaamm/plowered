"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  Controls,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  useReactFlow,
  type Connection,
  type Edge,
  type EdgeChange,
  type Node,
  type NodeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  InlineDrawer,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Subtitle2,
  Switch,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  AutoFitHeight20Regular,
  Code20Regular,
  Database20Regular,
  Delete20Regular,
  Dismiss20Regular,
  DocumentBulletList20Regular,
  Flow20Regular,
  Globe20Regular,
  Layer20Regular,
} from "@fluentui/react-icons";
import { InfoLabel } from "@/components/info-label";
import type { Task, TaskType } from "@/lib/types-orchestration";

// PALETTE describes the task types a user can drop into the DAG.
// Adding a new type means appending here + registering a runner in the
// worker — no other UI changes needed.
const PALETTE: {
  type: TaskType;
  label: string;
  desc: string;
  icon: React.ReactNode;
}[] = [
  { type: "sql",            label: "SQL",            desc: "Run a SQL statement on a connection",       icon: <Code20Regular /> },
  { type: "transform_run",  label: "Transform",      desc: "Materialise a dbt / SQL transform",         icon: <Flow20Regular /> },
  { type: "connector_sync", label: "Connector sync", desc: "Pull a source → warehouse",                 icon: <Database20Regular /> },
  { type: "quality_check",  label: "Quality check",  desc: "Run a check defined in the Quality module", icon: <DocumentBulletList20Regular /> },
  { type: "webhook",        label: "Webhook",        desc: "POST a JSON payload to an external URL",    icon: <Globe20Regular /> },
];

const HORIZONTAL_GAP = 240;
const VERTICAL_GAP = 110;

// detectCycle returns the set of node IDs on a cycle (empty if DAG).
// Cheap variant of Tarjan's: BFS-based topo sort drops everything that
// can be sorted; whatever remains is on a cycle.
function detectCycle(tasks: Task[]): Set<string> {
  const indeg = new Map<string, number>();
  const out = new Map<string, string[]>();
  for (const t of tasks) {
    indeg.set(t.ID, (t.DependsOn ?? []).length);
    out.set(t.ID, []);
  }
  for (const t of tasks) {
    for (const d of t.DependsOn ?? []) {
      out.get(d)?.push(t.ID);
    }
  }
  let frontier = tasks
    .filter((t) => (indeg.get(t.ID) ?? 0) === 0)
    .map((t) => t.ID);
  const removed = new Set<string>();
  while (frontier.length) {
    const next: string[] = [];
    for (const id of frontier) {
      removed.add(id);
      for (const child of out.get(id) ?? []) {
        const remaining = (indeg.get(child) ?? 0) - 1;
        indeg.set(child, remaining);
        if (remaining === 0) next.push(child);
      }
    }
    frontier = next;
  }
  const stuck = new Set<string>();
  for (const t of tasks) if (!removed.has(t.ID)) stuck.add(t.ID);
  return stuck;
}

// ADF-style placement: every new node lands just to the right of the
// rightmost existing one, at the same y. On an empty canvas the first
// node lands at a fixed visible spot. The viewport auto-fits after
// each add so users never have to pan to find what they just added.
function nextNodePositionFromNodes(
  added: Node[],
  existing: Node[],
): { x: number; y: number } {
  const all = [...added, ...existing];
  if (all.length === 0) return { x: 60, y: 60 };
  let bestX = -Infinity;
  let anchorY = 60;
  for (const n of all) {
    if (n.position.x > bestX) {
      bestX = n.position.x;
      anchorY = n.position.y;
    }
  }
  if (bestX === -Infinity) return { x: 60, y: 60 };
  return { x: bestX + HORIZONTAL_GAP, y: anchorY };
}

// Cheap probes into a node's data/style to decide whether it needs
// rebuilding during the tasks → nodes reconciliation. The encoding
// matches what buildFlow() emits.
function getNodeCycleFlag(n: Node): boolean {
  const bg = (n.style as { background?: string } | undefined)?.background;
  return bg === "#FFE9E5";
}
function getNodeTaskType(n: Node): string | undefined {
  return (n.data as { taskType?: string } | undefined)?.taskType;
}

// Topological auto-arrange (left → right). Each level becomes a column;
// nodes within a level stack vertically and are centred around y=0 so
// fitView frames the whole graph nicely.
function autoArrangePositions(tasks: Task[]): Record<string, { x: number; y: number }> {
  const indeg = new Map<string, number>();
  const out = new Map<string, string[]>();
  for (const t of tasks) {
    indeg.set(t.ID, (t.DependsOn ?? []).length);
    out.set(t.ID, []);
  }
  for (const t of tasks) {
    for (const d of t.DependsOn ?? []) out.get(d)?.push(t.ID);
  }
  const levels: string[][] = [];
  let frontier = tasks
    .filter((t) => (indeg.get(t.ID) ?? 0) === 0)
    .map((t) => t.ID)
    .sort();
  while (frontier.length) {
    levels.push(frontier);
    const next: string[] = [];
    for (const id of frontier) {
      for (const child of out.get(id) ?? []) {
        const r = (indeg.get(child) ?? 0) - 1;
        indeg.set(child, r);
        if (r === 0) next.push(child);
      }
    }
    frontier = next.sort();
  }
  // Cycle nodes (anything left over): drop into a column to the right.
  const seen = new Set(levels.flat());
  const stray = tasks.filter((t) => !seen.has(t.ID)).map((t) => t.ID);
  if (stray.length) levels.push(stray);

  const out2: Record<string, { x: number; y: number }> = {};
  levels.forEach((col, ci) => {
    col.forEach((id, ri) => {
      out2[id] = {
        x: 60 + ci * HORIZONTAL_GAP,
        y: 60 + ri * VERTICAL_GAP - ((col.length - 1) * VERTICAL_GAP) / 2,
      };
    });
  });
  return out2;
}

// Build React Flow nodes + edges. Position comes from the persisted
// state — buildFlow never falls back to a synthetic position, callers
// must seed positions before passing tasks in.
function buildFlow(
  tasks: Task[],
  positions: Record<string, { x: number; y: number }>,
  cycleNodes: Set<string>,
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = tasks.map((t) => {
    const onCycle = cycleNodes.has(t.ID);
    return {
      id: t.ID,
      position: positions[t.ID] ?? { x: 60, y: 60 },
      // ADF flows left → right: handles on the left (input) and right
      // (output) edges of each card.
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      data: {
        // taskType is read by the reconciler to decide whether the
        // node needs rebuilding (Type changes are rare; positions
        // change every drag frame).
        taskType: t.Type,
        label: (
          <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
            <span style={{ fontSize: 12, fontWeight: 700 }}>{t.ID}</span>
            <span style={{ fontSize: 11, color: "#7A6A55" }}>{t.Type}</span>
          </div>
        ),
      },
      style: {
        background: onCycle ? "#FFE9E5" : "#FFFFFF",
        border: `1.5px solid ${onCycle ? "#C03B2C" : "#F38020"}`,
        borderRadius: 8,
        padding: "10px 14px",
        width: 200,
        boxShadow: "0 1px 3px rgba(0,0,0,0.06), 0 1px 2px rgba(0,0,0,0.04)",
      },
    };
  });

  const edges: Edge[] = tasks.flatMap((t) =>
    (t.DependsOn ?? []).map((src) => ({
      id: `${src}->${t.ID}`,
      source: src,
      target: t.ID,
      style: { stroke: "#F38020", strokeWidth: 1.75 },
      type: "smoothstep",
      // animated=true triggers xyflow's built-in "marching ants"
      // stroke-dashoffset animation — solid path becomes a dashed line
      // flowing in the direction of the dependency. Matches ADF's
      // live-pipeline edge style.
      animated: true,
    })),
  );
  return { nodes, edges };
}

// -------- Editor --------
const useStyles = makeStyles({
  // Outer card: rounded box with a 1px outline. Inside it three rows
  // stack vertically: top bar (full width) → palette + canvas (+ drawer)
  // grid → hint bar (full width). Mirrors ADF's pipeline-editor chrome.
  editor: {
    display: "flex",
    flexDirection: "column",
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "8px",
    overflow: "hidden",
    height: "640px",
  },
  topBar: {
    padding: "10px 16px",
    minHeight: "48px",
    display: "flex",
    gap: "10px",
    alignItems: "center",
    backgroundColor: tokens.colorNeutralBackground2,
    borderBottomWidth: "1px",
    borderBottomStyle: "solid",
    borderBottomColor: tokens.colorNeutralStroke2,
  },
  shell: {
    display: "grid",
    flex: 1,
    minHeight: 0,
    // gridTemplateColumns set inline — 3-col when the task drawer is
    // open, 2-col otherwise. Keeps the canvas always interactive.
  },
  palette: {
    backgroundColor: tokens.colorNeutralBackground1,
    borderRightWidth: "1px",
    borderRightStyle: "solid",
    borderRightColor: tokens.colorNeutralStroke2,
    padding: "12px",
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    overflowY: "auto",
  },
  paletteItem: {
    display: "flex",
    gap: "10px",
    padding: "8px 10px",
    borderRadius: "4px",
    backgroundColor: tokens.colorNeutralBackground2,
    cursor: "grab",
    border: "none",
    textAlign: "left",
    color: tokens.colorNeutralForeground1,
    ":hover": { backgroundColor: "#FEF4E8" },
  },
  canvas: {
    backgroundColor: tokens.colorNeutralBackground1,
    overflow: "hidden",
    minWidth: 0,
  },
  drawer: {
    borderLeftWidth: "1px",
    borderLeftStyle: "solid",
    borderLeftColor: tokens.colorNeutralStroke2,
    overflow: "hidden",
    backgroundColor: tokens.colorNeutralBackground1,
  },
  toolbarDivider: {
    width: "1px",
    height: "20px",
    backgroundColor: tokens.colorNeutralStroke2,
    margin: "0 4px",
  },
  hintBar: {
    padding: "6px 12px",
    fontSize: "11px",
    color: tokens.colorNeutralForeground3,
    borderTopWidth: "1px",
    borderTopStyle: "solid",
    borderTopColor: tokens.colorNeutralStroke2,
    backgroundColor: tokens.colorNeutralBackground2,
    display: "flex",
    gap: "16px",
  },
  drawerSection: { display: "flex", flexDirection: "column", gap: "12px" },
  drawerCode: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    minHeight: "120px",
  },
});

export interface DAGEditorProps {
  tasks: Task[];
  onChange: (tasks: Task[]) => void;
}

// DAGEditor lets the user build a Pipeline.tasks DAG visually. Drag a
// type from the palette onto a fresh node; connect nodes by dragging
// from one's right edge to another's left. Each node has a settings
// drawer for its type-specific config + retry policy + dependency list.
//
// Cycles are highlighted red and prevent saving (validation happens at
// the parent component on submit).
// Public wrapper — provides the React Flow context so the inner
// component (and the palette + toolbar that live next to ReactFlow,
// not inside it) can use `useReactFlow()` for fitView / viewport ops.
export function DAGEditor(props: DAGEditorProps) {
  return (
    <ReactFlowProvider>
      <DAGEditorInner {...props} />
    </ReactFlowProvider>
  );
}

function DAGEditorInner({ tasks, onChange }: DAGEditorProps) {
  const styles = useStyles();
  const rf = useReactFlow();
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [configMode, setConfigMode] = useState(false);
  // Tracks whether we've run the initial mount-time fitView so it
  // doesn't fire on every render with non-empty tasks.
  const didInitialFit = useRef(false);

  const cycleNodes = useMemo(() => detectCycle(tasks), [tasks]);

  // ────────────────────────────────────────────────────────────────
  // Controlled-flow state per xyflow docs.
  //
  // The old useMemo(buildFlow(tasks, positions, …)) pattern caused
  // node *flicker* during drag: every position event triggered a
  // setState → re-render → buildFlow rebuilt every node with brand-
  // new `data` and `style` objects, which restarted React Flow's CSS
  // transitions. By keeping nodes/edges in useState and mutating only
  // the moved node's `position` field via applyNodeChanges, other
  // nodes keep their object identity and don't repaint.
  // ────────────────────────────────────────────────────────────────
  const [nodes, setNodes] = useState<Node[]>(() => buildFlow(tasks, {}, cycleNodes).nodes);
  const [edges, setEdges] = useState<Edge[]>(() => buildFlow(tasks, {}, cycleNodes).edges);

  // Reconcile nodes whenever tasks (add/remove/rename) or cycle
  // membership change. Preserves the position of any node that's
  // already in state — only the deltas get fresh objects.
  useEffect(() => {
    setNodes((current) => {
      const byID = new Map(current.map((n) => [n.id, n] as const));
      const next: Node[] = [];
      for (let i = 0; i < tasks.length; i++) {
        const t = tasks[i];
        const onCycle = cycleNodes.has(t.ID);
        const existing = byID.get(t.ID);
        const position =
          existing?.position ?? nextNodePositionFromNodes(next, current);
        const built = buildFlow([t], { [t.ID]: position }, onCycle ? new Set([t.ID]) : new Set()).nodes[0];
        // Preserve the existing object identity if nothing meaningful
        // changed (same position, same cycle status, same type label).
        if (
          existing &&
          existing.position.x === position.x &&
          existing.position.y === position.y &&
          getNodeCycleFlag(existing) === onCycle &&
          getNodeTaskType(existing) === t.Type
        ) {
          next.push(existing);
        } else {
          next.push(built);
        }
      }
      return next;
    });
    setEdges(buildFlow(tasks, {}, cycleNodes).edges);
  }, [tasks, cycleNodes]);

  // onNodesChange is now a thin wrapper around applyNodeChanges. The
  // only side-effects we run are mirroring `remove` events into the
  // tasks list and capturing `select` events for the config drawer.
  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      setNodes((current) => applyNodeChanges(changes, current));

      // Mirror node removals into tasks.
      const removedIds: string[] = [];
      for (const c of changes) {
        if (c.type === "remove") removedIds.push(c.id);
        else if (c.type === "select" && c.selected && configMode) {
          setSelectedID(c.id);
        }
      }
      if (removedIds.length) {
        const filtered = tasks
          .filter((t) => !removedIds.includes(t.ID))
          .map((t) => ({
            ...t,
            DependsOn: (t.DependsOn ?? []).filter((d) => !removedIds.includes(d)),
          }));
        onChange(filtered);
      }
    },
    [tasks, onChange, configMode],
  );

  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => {
      let next = tasks;
      for (const c of changes) {
        if (c.type === "remove") {
          // edge id format: "<src>-><tgt>"
          const [src, tgt] = c.id.split("->");
          next = next.map((t) =>
            t.ID === tgt
              ? { ...t, DependsOn: (t.DependsOn ?? []).filter((d) => d !== src) }
              : t,
          );
        }
      }
      if (next !== tasks) onChange(next);
      setEdges((current) => applyEdgeChanges(changes, current));
    },
    [tasks, onChange],
  );

  const onConnect = useCallback(
    (params: Connection) => {
      if (!params.source || !params.target || params.source === params.target) {
        return;
      }
      const next = tasks.map((t) => {
        if (t.ID !== params.target) return t;
        const deps = new Set(t.DependsOn ?? []);
        deps.add(params.source!);
        return { ...t, DependsOn: Array.from(deps) };
      });
      onChange(next);
      setEdges((current) => addEdge({ ...params, animated: true }, current));
    },
    [tasks, onChange],
  );

  const addTask = (type: TaskType) => {
    const base = type;
    let n = 1;
    while (tasks.some((t) => t.ID === `${base}_${n}`)) n++;
    const id = `${base}_${n}`;
    onChange([
      ...tasks,
      {
        ID: id,
        Type: type,
        Config: {},
        DependsOn: [],
      } as Task,
    ]);
    // The new node's position is seeded by the tasks→nodes reconciler
    // (nextNodePositionFromNodes places it right of the rightmost
    // existing node). After it renders, animate the viewport so the
    // user always sees what they just added.
    requestAnimationFrame(() => {
      rf.fitView({ padding: 0.25, duration: 300 }).catch(() => {});
    });
  };

  // Toolbar action: tidy up the canvas into a clean left → right
  // topological layout. Useful when the user has dragged things around
  // and wants to snap everything back to the columns.
  const onAutoArrange = () => {
    const fresh = autoArrangePositions(tasks);
    setNodes((current) =>
      current.map((n) => {
        const p = fresh[n.id];
        return p ? { ...n, position: p } : n;
      }),
    );
    requestAnimationFrame(() => {
      rf.fitView({ padding: 0.2, duration: 400 }).catch(() => {});
    });
  };

  const onFitView = () => {
    rf.fitView({ padding: 0.2, duration: 250 }).catch(() => {});
  };

  // First-mount fit: if the editor loads with existing tasks (e.g.
  // editing a saved pipeline), make sure they're framed.
  useEffect(() => {
    if (didInitialFit.current) return;
    if (tasks.length === 0) return;
    didInitialFit.current = true;
    // Defer to give the layout one frame to settle.
    requestAnimationFrame(() => {
      rf.fitView({ padding: 0.25, duration: 0 }).catch(() => {});
    });
  }, [tasks.length, rf]);

  const updateSelected = (patch: Partial<Task>) => {
    if (!selectedID) return;
    onChange(
      tasks.map((t) => (t.ID === selectedID ? { ...t, ...patch } as Task : t)),
    );
  };

  const removeSelected = () => {
    if (!selectedID) return;
    onChange(
      tasks
        .filter((t) => t.ID !== selectedID)
        .map((t) => ({
          ...t,
          DependsOn: (t.DependsOn ?? []).filter((d) => d !== selectedID),
        })),
    );
    setSelectedID(null);
  };

  // Keep the selected task in sync with state edits.
  const selected = useMemo(
    () => tasks.find((t) => t.ID === selectedID) ?? null,
    [tasks, selectedID],
  );

  // When the user deletes the last task, also clear the drawer.
  useEffect(() => {
    if (selectedID && !tasks.some((t) => t.ID === selectedID)) {
      setSelectedID(null);
    }
  }, [tasks, selectedID]);

  return (
    <div>
      {cycleNodes.size > 0 && (
        <MessageBar intent="error" style={{ marginBottom: 8 }}>
          <MessageBarBody>
            Cycle detected on{" "}
            <strong>{Array.from(cycleNodes).join(", ")}</strong>. Remove a
            dependency to make this a DAG.
          </MessageBarBody>
        </MessageBar>
      )}

      <div className={styles.editor}>
        {/* ── Full-width top toolbar (ADF-style header) ──────────── */}
        <div className={styles.topBar}>
          {/* Status group */}
          <Badge appearance="outline" color="brand">
            {tasks.length} task{tasks.length === 1 ? "" : "s"}
          </Badge>
          {cycleNodes.size === 0 && tasks.length > 0 && (
            <Badge appearance="filled" color="success">
              valid DAG
            </Badge>
          )}
          {cycleNodes.size > 0 && (
            <Badge appearance="filled" color="danger">
              cycle detected
            </Badge>
          )}

          <div className={styles.toolbarDivider} />

          {/* Layout group */}
          <Button
            size="small"
            appearance="subtle"
            icon={<Layer20Regular />}
            onClick={onAutoArrange}
            disabled={tasks.length === 0}
            title="Re-layout left → right by topological level"
          >
            Auto-arrange
          </Button>
          <Button
            size="small"
            appearance="subtle"
            icon={<AutoFitHeight20Regular />}
            onClick={onFitView}
            disabled={tasks.length === 0}
            title="Frame the whole graph in the viewport"
          >
            Fit to screen
          </Button>

          <div className={styles.toolbarDivider} />

          {/* Delete group */}
          <Button
            size="small"
            appearance="subtle"
            icon={<Delete20Regular />}
            onClick={removeSelected}
            disabled={!selectedID}
            title={
              selectedID
                ? `Remove ${selectedID} from the pipeline`
                : "Select a node to delete it"
            }
          >
            Delete
          </Button>

          <div style={{ flex: 1 }} />

          {/* View-mode group */}
          <Switch
            checked={configMode}
            onChange={(_, d) => {
              setConfigMode(d.checked);
              if (!d.checked) setSelectedID(null);
            }}
            label={
              <InfoLabel info="Off: drag nodes freely; clicks just select. On: clicking a node opens its config drawer.">
                Config mode
              </InfoLabel>
            }
          />
        </div>

        {/* ── Middle row: palette · canvas · (drawer) ─────────── */}
        <div
          className={styles.shell}
          style={{
            gridTemplateColumns:
              configMode && selected ? "220px 1fr 420px" : "220px 1fr",
          }}
        >
          <aside className={styles.palette}>
            <Subtitle2>Activities</Subtitle2>
            <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
              Click to add an activity. Drag-connect nodes to wire dependencies.
            </Caption1>
            {PALETTE.map((p) => (
              <button
                key={p.type}
                type="button"
                className={styles.paletteItem}
                onClick={() => addTask(p.type)}
              >
                <span style={{ color: "#F38020" }}>{p.icon}</span>
                <div>
                  <div style={{ fontWeight: 600, fontSize: 13 }}>{p.label}</div>
                  <div style={{ fontSize: 11, color: "#7A6A55" }}>{p.desc}</div>
                </div>
              </button>
            ))}
          </aside>

          <div className={styles.canvas}>
            <ReactFlow
              nodes={nodes}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              onNodeClick={(_, n) => {
                if (configMode) setSelectedID(n.id);
              }}
              fitView
              fitViewOptions={{ maxZoom: 1, padding: 0.25 }}
              minZoom={0.2}
              maxZoom={1.5}
              proOptions={{ hideAttribution: true }}
              defaultEdgeOptions={{ type: "smoothstep", animated: true }}
              deleteKeyCode={["Backspace", "Delete"]}
              nodesDraggable
              nodesConnectable
              elementsSelectable
            >
              <Background gap={16} size={1} />
              <Controls />
              <MiniMap pannable zoomable />
            </ReactFlow>
          </div>

          {/* Task settings drawer — only mounts while Config mode is
              on AND a node is selected, otherwise it'd wrap to row 2
              of the grid (the parent only carves out a 3rd column in
              that case). */}
          {configMode && selected && (
          <InlineDrawer
            open
            position="end"
            separator
            className={styles.drawer}
          >
        <DrawerHeader>
          <DrawerHeaderTitle
            action={
              <Button
                appearance="subtle"
                icon={<Dismiss20Regular />}
                onClick={() => setSelectedID(null)}
                aria-label="Close"
              />
            }
          >
            {selected ? `Task · ${selected.ID}` : ""}
          </DrawerHeaderTitle>
        </DrawerHeader>
        {selected && (
          <DrawerBody>
            <div className={styles.drawerSection}>
              <Field label="ID" required hint="Unique within the pipeline. Used as the run identifier.">
                <Input
                  value={selected.ID}
                  onChange={(_, d) => {
                    const newID = d.value.replace(/[^a-zA-Z0-9_]/g, "");
                    if (!newID || tasks.some((t) => t.ID === newID && t.ID !== selected.ID)) {
                      return;
                    }
                    onChange(
                      tasks.map((t) => {
                        if (t.ID === selected.ID) return { ...t, ID: newID };
                        return {
                          ...t,
                          DependsOn: (t.DependsOn ?? []).map((d2) =>
                            d2 === selected.ID ? newID : d2,
                          ),
                        };
                      }),
                    );
                    // Migrate the node ID in flow state so its
                    // position is preserved across the rename.
                    setNodes((current) =>
                      current.map((n) =>
                        n.id === selected.ID ? { ...n, id: newID } : n,
                      ),
                    );
                    setEdges((current) =>
                      current.map((e) => ({
                        ...e,
                        source: e.source === selected.ID ? newID : e.source,
                        target: e.target === selected.ID ? newID : e.target,
                        id:
                          e.source === selected.ID || e.target === selected.ID
                            ? `${e.source === selected.ID ? newID : e.source}->${e.target === selected.ID ? newID : e.target}`
                            : e.id,
                      })),
                    );
                    setSelectedID(newID);
                  }}
                />
              </Field>
              <Field label="Type">
                <Body1>
                  <Badge appearance="outline" color="brand">{selected.Type}</Badge>
                </Body1>
              </Field>
              <Field
                label="Config (JSON)"
                hint="Type-specific. SQL takes {connection_id, sql}; webhook takes {url, body}; etc."
              >
                <Textarea
                  className={styles.drawerCode}
                  value={JSON.stringify(selected.Config ?? {}, null, 2)}
                  onChange={(_, d) => {
                    try {
                      const parsed = JSON.parse(d.value || "{}");
                      updateSelected({ Config: parsed });
                    } catch {
                      // hold the typing error silently — user can fix
                    }
                  }}
                />
              </Field>
              <Field label="Depends on" hint="IDs of tasks that must finish first.">
                <Input
                  value={(selected.DependsOn ?? []).join(", ")}
                  onChange={(_, d) =>
                    updateSelected({
                      DependsOn: d.value
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    })
                  }
                  placeholder="extract, transform"
                />
              </Field>
              <div style={{ display: "flex", justifyContent: "flex-end" }}>
                <Button
                  appearance="secondary"
                  icon={<Delete20Regular />}
                  onClick={removeSelected}
                >
                  Remove task
                </Button>
              </div>
            </div>
          </DrawerBody>
        )}
        </InlineDrawer>
          )}
        </div>

        {/* ── Full-width hint bar ─────────────────────────────── */}
        <div className={styles.hintBar}>
          <span>
            <strong>Drag</strong> the canvas to pan · <strong>scroll</strong> to zoom
          </span>
          <span>
            <strong>Drag</strong> from a node's right edge to wire a dependency
          </span>
          <span>
            <strong>Delete</strong> / <strong>Backspace</strong> removes the selected node
          </span>
        </div>
      </div>
    </div>
  );
}

// helper exposed for the parent's "Add" button when there are zero tasks
export function FirstTaskButton({
  onAdd,
}: {
  onAdd: (type: TaskType) => void;
}) {
  return (
    <Button
      appearance="primary"
      icon={<Add20Regular />}
      onClick={() => onAdd("sql")}
    >
      Add first task
    </Button>
  );
}
