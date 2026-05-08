"use client";

import { useMemo } from "react";
import Link from "next/link";
import { makeStyles, tokens } from "@fluentui/react-components";
import type { Asset, LineageResponse } from "@/lib/types";

// LineageGraph renders a three-column hierarchical lineage view in pure SVG:
// upstream neighbors on the left, the root in the middle, downstream on the
// right. Layout is deterministic — no animation library, no force solver —
// because v0 needs reviewability over fanciness.

const NODE_WIDTH = 240;
const NODE_HEIGHT = 56;
const COLUMN_GAP = 96;
const ROW_GAP = 16;
const PADDING = 24;

const useStyles = makeStyles({
  wrap: {
    width: "100%",
    overflow: "auto",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground1,
  },
  empty: {
    padding: "24px",
    color: tokens.colorNeutralForeground2,
    fontSize: tokens.fontSizeBase200,
  },
});

type Column = "upstream" | "root" | "downstream";

interface PositionedNode {
  asset: Asset;
  column: Column;
  x: number;
  y: number;
}

export function LineageGraph({ data }: { data: LineageResponse }) {
  const styles = useStyles();
  const layout = useMemo(() => buildLayout(data), [data]);

  if (!layout) {
    return <div className={styles.empty}>No lineage edges yet.</div>;
  }

  const { width, height, nodes, paths } = layout;

  return (
    <div className={styles.wrap}>
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={`Lineage for ${data.root.qualified_name}`}
      >
        <defs>
          <marker
            id="arrow"
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="6"
            markerHeight="6"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 10 5 L 0 10 z" fill={tokens.colorNeutralStroke1} />
          </marker>
        </defs>

        {paths.map((p, i) => (
          <path
            key={i}
            d={p}
            fill="none"
            stroke={tokens.colorNeutralStroke1}
            strokeWidth={1.5}
            markerEnd="url(#arrow)"
          />
        ))}

        {nodes.map((n) => (
          <NodeRect key={n.asset.id} node={n} isRoot={n.column === "root"} />
        ))}
      </svg>
    </div>
  );
}

function NodeRect({ node, isRoot }: { node: PositionedNode; isRoot: boolean }) {
  const fill = isRoot ? tokens.colorBrandBackground2 : tokens.colorNeutralBackground2;
  const stroke = isRoot ? tokens.colorBrandStroke2 : tokens.colorNeutralStroke1;
  const textColor = isRoot ? tokens.colorBrandForeground1 : tokens.colorNeutralForeground1;
  const subColor = tokens.colorNeutralForeground2;

  const link = `/asset/${encodeURIComponent(node.asset.qualified_name)}`;

  return (
    <Link href={link}>
      <g transform={`translate(${node.x}, ${node.y})`} style={{ cursor: "pointer" }}>
        <rect
          width={NODE_WIDTH}
          height={NODE_HEIGHT}
          rx={6}
          ry={6}
          fill={fill}
          stroke={stroke}
          strokeWidth={1.5}
        />
        <text
          x={12}
          y={22}
          fontSize={11}
          fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
          fill={subColor}
        >
          {truncate(node.asset.qualified_name, 36)}
        </text>
        <text
          x={12}
          y={42}
          fontSize={14}
          fontWeight={600}
          fill={textColor}
        >
          {truncate(node.asset.name, 30)}
        </text>
      </g>
    </Link>
  );
}

interface Layout {
  width: number;
  height: number;
  nodes: PositionedNode[];
  paths: string[];
}

function buildLayout(data: LineageResponse): Layout | null {
  const byId = new Map<string, Asset>();
  byId.set(data.root.id, data.root);
  for (const n of data.neighbors) byId.set(n.id, n);

  // Partition neighbors by direction relative to root.
  const upstream: Asset[] = [];
  const downstream: Asset[] = [];
  for (const e of data.edges) {
    if (e.target_id === data.root.id) {
      const a = byId.get(e.source_id);
      if (a && !upstream.includes(a)) upstream.push(a);
    } else if (e.source_id === data.root.id) {
      const a = byId.get(e.target_id);
      if (a && !downstream.includes(a)) downstream.push(a);
    }
  }
  if (upstream.length === 0 && downstream.length === 0) return null;

  const rows = Math.max(upstream.length, downstream.length, 1);
  const totalH = PADDING * 2 + rows * NODE_HEIGHT + (rows - 1) * ROW_GAP;
  const totalW = PADDING * 2 + NODE_WIDTH * 3 + COLUMN_GAP * 2;

  const placeColumn = (assets: Asset[], col: Column, xCol: number): PositionedNode[] => {
    const startY = PADDING + (totalH - PADDING * 2 - assets.length * NODE_HEIGHT - (assets.length - 1) * ROW_GAP) / 2;
    return assets.map((a, i) => ({
      asset: a,
      column: col,
      x: xCol,
      y: startY + i * (NODE_HEIGHT + ROW_GAP),
    }));
  };

  const upX = PADDING;
  const rootX = PADDING + NODE_WIDTH + COLUMN_GAP;
  const downX = PADDING + NODE_WIDTH * 2 + COLUMN_GAP * 2;

  const upNodes = placeColumn(upstream, "upstream", upX);
  const rootNode: PositionedNode = {
    asset: data.root,
    column: "root",
    x: rootX,
    y: (totalH - NODE_HEIGHT) / 2,
  };
  const downNodes = placeColumn(downstream, "downstream", downX);

  const nodes = [...upNodes, rootNode, ...downNodes];
  const lookup = new Map(nodes.map((n) => [n.asset.id, n]));

  const paths: string[] = [];
  for (const e of data.edges) {
    const src = lookup.get(e.source_id);
    const tgt = lookup.get(e.target_id);
    if (!src || !tgt) continue;
    const x1 = src.x + NODE_WIDTH;
    const y1 = src.y + NODE_HEIGHT / 2;
    const x2 = tgt.x;
    const y2 = tgt.y + NODE_HEIGHT / 2;
    const midX = (x1 + x2) / 2;
    paths.push(`M ${x1} ${y1} C ${midX} ${y1}, ${midX} ${y2}, ${x2} ${y2}`);
  }

  return { width: totalW, height: totalH, nodes, paths };
}

function truncate(s: string, n: number) {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}
