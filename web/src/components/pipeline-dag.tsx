// PipelineDAG renders a Pipeline's tasks as a left-to-right React Flow
// graph. Layout is computed deterministically from DependsOn — a Sugiyama
// layered layout where each level is a column.

"use client";

import { useMemo } from "react";
import {
  Background,
  Controls,
  ReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import type { Task } from "@/lib/types-orchestration";

const HORIZONTAL_GAP = 240;
const VERTICAL_GAP = 90;

function topoLevels(tasks: Task[]): string[][] {
  // BFS by depth from roots. Same algorithm as the Go runner, simplified.
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

export function PipelineDAG({ tasks }: { tasks: Task[] }) {
  const { nodes, edges } = useMemo(() => {
    const levels = topoLevels(tasks);
    const positions = new Map<string, { x: number; y: number }>();
    levels.forEach((level, col) => {
      level.forEach((id, row) => {
        positions.set(id, {
          x: col * HORIZONTAL_GAP,
          y:
            row * VERTICAL_GAP -
            ((level.length - 1) * VERTICAL_GAP) / 2,
        });
      });
    });

    const nodes: Node[] = tasks.map((t) => ({
      id: t.ID,
      position: positions.get(t.ID) ?? { x: 0, y: 0 },
      data: { label: `${t.ID}\n${t.Type}` },
      style: {
        background: "#FAFAFA",
        border: "1px solid #F38020",
        borderRadius: 6,
        padding: "10px 14px",
        fontSize: 12,
        whiteSpace: "pre-line",
        textAlign: "center",
        width: 180,
      },
    }));

    const edges: Edge[] = tasks.flatMap((t) =>
      (t.DependsOn ?? []).map((src) => ({
        id: `${src}->${t.ID}`,
        source: src,
        target: t.ID,
        animated: false,
        style: { stroke: "#F38020" },
      })),
    );
    return { nodes, edges };
  }, [tasks]);

  if (tasks.length === 0) {
    return (
      <div style={{ padding: 24, color: "#7A6A55" }}>
        No tasks defined.
      </div>
    );
  }

  return (
    <div style={{ height: 360, border: "1px solid #E5E7EB", borderRadius: 8 }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        fitViewOptions={{ maxZoom: 1, padding: 0.3 }}
        minZoom={0.2}
        maxZoom={1.5}
        proOptions={{ hideAttribution: true }}
        defaultEdgeOptions={{ type: "smoothstep" }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
      >
        <Background />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  );
}
