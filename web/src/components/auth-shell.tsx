"use client";

import { ReactNode } from "react";
import {
  Body1,
  Caption1,
  Title2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";

const useStyles = makeStyles({
  // Two-column shell. On narrow viewports the right pane collapses and
  // the form pane takes the full width.
  page: {
    minHeight: "100vh",
    width: "100%",
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)",
    backgroundColor: "#F5F5F5",
    "@media (max-width: 880px)": {
      gridTemplateColumns: "1fr",
    },
  },
  // The form pane is a positioned shell: brand is pinned top-left,
  // footer pinned bottom-center, and the form itself is vertically +
  // horizontally centered in the available space. No card chrome —
  // fields sit directly on the cream canvas.
  formPane: {
    position: "relative",
    minHeight: "100vh",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    padding: "40px 32px",
    backgroundColor: "#FAFAF8",
  },
  brand: {
    position: "absolute",
    top: "32px",
    left: "32px",
    display: "flex",
    alignItems: "center",
    gap: "10px",
    color: tokens.colorBrandForeground1,
    fontWeight: 700,
    fontSize: "16px",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    letterSpacing: "0.02em",
  },
  brandDot: {
    width: "12px",
    height: "12px",
    borderRadius: "2px",
    backgroundColor: tokens.colorBrandBackground,
  },
  card: {
    width: "100%",
    maxWidth: "400px",
    display: "flex",
    flexDirection: "column",
    gap: "24px",
  },
  head: { display: "flex", flexDirection: "column", gap: "8px" },
  subtitle: { color: tokens.colorNeutralForeground3 },
  footer: {
    position: "absolute",
    bottom: "24px",
    left: "0",
    right: "0",
    display: "flex",
    justifyContent: "center",
    color: tokens.colorNeutralForeground3,
    fontSize: "11px",
  },
  // Right pane: gradient canvas + animated lineage graph + tagline.
  artPane: {
    position: "relative",
    overflow: "hidden",
    minHeight: "100vh",
    backgroundImage:
      "linear-gradient(135deg, #2A1810 0%, #5C2E15 45%, #B5491A 100%)",
    color: "#FAEDD8",
    display: "flex",
    flexDirection: "column",
    justifyContent: "flex-end",
    padding: "48px",
    "@media (max-width: 880px)": {
      display: "none",
    },
  },
  artSvg: {
    position: "absolute",
    inset: "0",
    width: "100%",
    height: "100%",
    opacity: "0.85",
  },
  artCopy: {
    position: "relative",
    zIndex: "1",
    maxWidth: "440px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
  },
  artTitle: {
    fontSize: "26px",
    fontWeight: "700",
    lineHeight: "1.25",
    color: "#FAEDD8",
  },
  artBody: {
    fontSize: "14px",
    color: "rgba(250,237,216,0.78)",
    lineHeight: "1.55",
  },
  artBadge: {
    display: "inline-flex",
    alignItems: "center",
    gap: "8px",
    padding: "6px 12px",
    borderRadius: "999px",
    backgroundColor: "rgba(250,237,216,0.08)",
    border: "1px solid rgba(250,237,216,0.18)",
    fontSize: "11px",
    letterSpacing: "0.08em",
    textTransform: "uppercase",
    color: "rgba(250,237,216,0.85)",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    width: "fit-content",
  },
});

export function AuthShell({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
}) {
  const styles = useStyles();
  return (
    <div className={styles.page}>
      <div className={styles.formPane}>
        <div className={styles.brand}>
          <span className={styles.brandDot} />
          <span>plowered</span>
        </div>
        <div className={styles.card}>
          <div className={styles.head}>
            <Title2 as="h1">{title}</Title2>
            {subtitle && <Body1 className={styles.subtitle}>{subtitle}</Body1>}
          </div>
          {children}
        </div>
        <Caption1 className={styles.footer}>
          © Plowered · data context platform
        </Caption1>
      </div>
      <div className={styles.artPane}>
        <LineageAnimation className={styles.artSvg} />
        <div className={styles.artCopy}>
          <span className={styles.artBadge}>context · lineage · trust</span>
          <div className={styles.artTitle}>
            Every table, every column, every query — connected.
          </div>
          <div className={styles.artBody}>
            Plowered keeps your data context in one graph so analysts, AI
            agents, and your compliance team see the same truth.
          </div>
        </div>
      </div>
    </div>
  );
}

// Decorative SVG: a small lineage graph. Edges are dashed strokes whose
// dash-offset drifts to suggest flow; nodes pulse with an outer ring
// that expands and fades. CSS keyframes live inside the SVG so the
// component stays self-contained.
function LineageAnimation({ className }: { className: string }) {
  const edges: Array<{ d: string; delay: string; duration: string }> = [
    { d: "M 120 180 C 220 140, 280 280, 380 260", delay: "0s",   duration: "5.5s" },
    { d: "M 380 260 C 460 280, 480 420, 560 440", delay: "0.6s", duration: "6.2s" },
    { d: "M 120 180 C 200 320, 320 500, 420 540", delay: "1.2s", duration: "7.0s" },
    { d: "M 380 260 C 320 200, 240 90,  140 100", delay: "1.8s", duration: "5.8s" },
    { d: "M 560 440 C 600 320, 540 200, 460 140", delay: "2.4s", duration: "6.5s" },
    { d: "M 420 540 C 480 480, 540 470, 560 440", delay: "3.0s", duration: "5.4s" },
  ];
  const nodes: Array<{ cx: number; cy: number; r: number; delay: string }> = [
    { cx: 120, cy: 180, r: 10, delay: "0s"   },
    { cx: 380, cy: 260, r: 12, delay: "0.5s" },
    { cx: 560, cy: 440, r: 10, delay: "1.0s" },
    { cx: 420, cy: 540, r: 8,  delay: "1.5s" },
    { cx: 140, cy: 100, r: 7,  delay: "2.0s" },
    { cx: 460, cy: 140, r: 9,  delay: "2.5s" },
  ];

  return (
    <svg
      className={className}
      viewBox="0 0 700 700"
      preserveAspectRatio="xMidYMid slice"
      aria-hidden="true"
    >
      <style>{`
        @keyframes plowered-flow {
          0%   { stroke-dashoffset: 200; opacity: 0.15; }
          25%  { opacity: 0.55; }
          75%  { opacity: 0.55; }
          100% { stroke-dashoffset: 0;   opacity: 0.15; }
        }
        @keyframes plowered-pulse {
          0%, 100% { transform: scale(1);    opacity: 0.95; }
          50%      { transform: scale(1.35); opacity: 0.55; }
        }
        @keyframes plowered-ring {
          0%   { transform: scale(1);   opacity: 0.55; }
          100% { transform: scale(3.2); opacity: 0;    }
        }
        .pl-edge {
          fill: none;
          stroke: rgba(250,237,216,0.5);
          stroke-width: 1.4;
          stroke-dasharray: 6 10;
          animation-name: plowered-flow;
          animation-iteration-count: infinite;
          animation-timing-function: linear;
        }
        .pl-node {
          fill: #FAEDD8;
          transform-origin: center;
          transform-box: fill-box;
          animation: plowered-pulse 3.4s ease-in-out infinite;
        }
        .pl-ring {
          fill: none;
          stroke: rgba(250,237,216,0.45);
          stroke-width: 1;
          transform-origin: center;
          transform-box: fill-box;
          animation: plowered-ring 3.6s ease-out infinite;
        }
      `}</style>

      {edges.map((e, i) => (
        <path
          key={`e-${i}`}
          className="pl-edge"
          d={e.d}
          style={{ animationDelay: e.delay, animationDuration: e.duration }}
        />
      ))}

      {nodes.map((n, i) => (
        <g key={`n-${i}`}>
          <circle
            className="pl-ring"
            cx={n.cx}
            cy={n.cy}
            r={n.r}
            style={{ animationDelay: n.delay }}
          />
          <circle
            className="pl-node"
            cx={n.cx}
            cy={n.cy}
            r={n.r}
            style={{ animationDelay: n.delay }}
          />
        </g>
      ))}
    </svg>
  );
}
