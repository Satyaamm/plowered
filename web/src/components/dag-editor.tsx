"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
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
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  Code20Regular,
  Database20Regular,
  Delete20Regular,
  Dismiss20Regular,
  DocumentBulletList20Regular,
  Flow20Regular,
  Globe20Regular,
} from "@fluentui/react-icons";
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

// -------- topological layout (matches the runner's BFS) --------
function topoLevels(tasks: Task[]): string[][] {
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
        const remaining = (indeg.get(child) ?? 0) - 1;
        indeg.set(child, remaining);
        if (remaining === 0) next.push(child);
      }
    }
    frontier = next.sort();
  }
  return levels;
}

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

// -------- task → React Flow node, edges from DependsOn --------
function tasksToFlow(tasks: Task[], cycleNodes: Set<string>) {
  const levels = topoLevels(tasks);
  const positions = new Map<string, { x: number; y: number }>();
  levels.forEach((level, col) => {
    level.forEach((id, row) => {
      positions.set(id, {
        x: col * HORIZONTAL_GAP,
        y: row * VERTICAL_GAP - ((level.length - 1) * VERTICAL_GAP) / 2,
      });
    });
  });
  // Anything stuck in a cycle has no level — push it into a column off
  // to the right with a red border.
  let strayCol = levels.length;
  for (const t of tasks) {
    if (!positions.has(t.ID)) {
      positions.set(t.ID, { x: strayCol * HORIZONTAL_GAP, y: 0 });
      strayCol++;
    }
  }

  const nodes: Node[] = tasks.map((t) => {
    const onCycle = cycleNodes.has(t.ID);
    return {
      id: t.ID,
      position: positions.get(t.ID) ?? { x: 0, y: 0 },
      data: {
        label: (
          <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
            <span style={{ fontSize: 12, fontWeight: 700 }}>{t.ID}</span>
            <span style={{ fontSize: 11, color: "#7A6A55" }}>{t.Type}</span>
          </div>
        ),
      },
      style: {
        background: onCycle ? "#FFE9E5" : "#FAFAFA",
        border: `1.5px solid ${onCycle ? "#C03B2C" : "#F38020"}`,
        borderRadius: 6,
        padding: "10px 14px",
        width: 200,
      },
    };
  });

  const edges: Edge[] = tasks.flatMap((t) =>
    (t.DependsOn ?? []).map((src) => ({
      id: `${src}->${t.ID}`,
      source: src,
      target: t.ID,
      style: { stroke: "#F38020", strokeWidth: 1.5 },
      type: "smoothstep",
    })),
  );
  return { nodes, edges };
}

// -------- Editor --------
const useStyles = makeStyles({
  shell: {
    display: "grid",
    // gridTemplateColumns is set inline below — switches to a 3-col
    // layout when the task drawer is open so the canvas stays
    // visible AND interactive next to the panel (inline drawer
    // pattern, not an overlay).
    gap: "12px",
    height: "560px",
  },
  drawer: {
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
    backgroundColor: tokens.colorNeutralBackground1,
  },
  palette: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
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
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
  },
  toolbar: {
    padding: "8px 12px",
    display: "flex",
    gap: "8px",
    alignItems: "center",
    borderBottomWidth: "1px",
    borderBottomStyle: "solid",
    borderBottomColor: tokens.colorNeutralStroke2,
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
export function DAGEditor({ tasks, onChange }: DAGEditorProps) {
  const styles = useStyles();
  const [selectedID, setSelectedID] = useState<string | null>(null);

  const cycleNodes = useMemo(() => detectCycle(tasks), [tasks]);
  const flow = useMemo(() => tasksToFlow(tasks, cycleNodes), [tasks, cycleNodes]);

  // React Flow drives node/edge state via callbacks; we keep the DAG
  // truth in our `tasks` prop. Position-only changes are dropped (we
  // recompute from topo levels), but selection + delete are honoured.
  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      let next = tasks;
      for (const c of changes) {
        if (c.type === "remove") {
          next = next.filter((t) => t.ID !== c.id);
          // also drop any DependsOn references
          next = next.map((t) => ({
            ...t,
            DependsOn: (t.DependsOn ?? []).filter((d) => d !== c.id),
          }));
        }
        if (c.type === "select") {
          if (c.selected) setSelectedID(c.id);
        }
      }
      if (next !== tasks) onChange(next);
      // Visual position changes are not persisted; React Flow handles them locally.
      void applyNodeChanges(changes, flow.nodes);
    },
    [tasks, onChange, flow.nodes],
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
      void applyEdgeChanges(changes, flow.edges);
    },
    [tasks, onChange, flow.edges],
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
      void addEdge(params, flow.edges);
    },
    [tasks, onChange, flow.edges],
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
    setSelectedID(id);
  };

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

      <div
        className={styles.shell}
        style={{
          gridTemplateColumns: selected ? "220px 1fr 420px" : "220px 1fr",
        }}
      >
        <aside className={styles.palette}>
          <Subtitle2>Task palette</Subtitle2>
          <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
            Click to add a task. Drag-connect nodes to wire dependencies.
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
          <div className={styles.toolbar}>
            <Badge appearance="outline" color="brand">
              {tasks.length} task{tasks.length === 1 ? "" : "s"}
            </Badge>
            {cycleNodes.size === 0 && tasks.length > 0 && (
              <Badge appearance="filled" color="success">
                valid DAG
              </Badge>
            )}
            <div style={{ flex: 1 }} />
            <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
              Drag from a node's right edge to wire a dependency. Click a node to edit it.
            </Caption1>
          </div>
          <div style={{ height: 510 }}>
            <ReactFlowProvider>
              <ReactFlow
                nodes={flow.nodes}
                edges={flow.edges}
                onNodesChange={onNodesChange}
                onEdgesChange={onEdgesChange}
                onConnect={onConnect}
                onNodeClick={(_, n) => setSelectedID(n.id)}
                fitView
                fitViewOptions={{ maxZoom: 1, padding: 0.3 }}
                minZoom={0.2}
                maxZoom={1.5}
                proOptions={{ hideAttribution: true }}
                defaultEdgeOptions={{ type: "smoothstep" }}
              >
                <Background />
                <Controls />
                <MiniMap pannable zoomable />
              </ReactFlow>
            </ReactFlowProvider>
          </div>
        </div>

        {/* Task settings drawer — inline (not overlay) so it occupies
            its own grid column and the canvas stays interactive while
            you edit a node. Same pattern n8n and Dagster use. */}
        <InlineDrawer open={!!selected} position="end" separator className={styles.drawer}>
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
