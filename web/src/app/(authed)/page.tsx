"use client";

import Link from "next/link";
import { useMemo } from "react";
import {
  Badge,
  Button,
  Caption1,
  DataGrid,
  DataGridBody,
  DataGridCell,
  DataGridHeader,
  DataGridHeaderCell,
  DataGridRow,
  Subtitle1,
  Subtitle2,
  TableColumnDefinition,
  createTableColumn,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Database24Filled,
  Flow24Filled,
  CheckmarkCircle24Filled,
  Eye24Filled,
  Delete24Filled,
  Gavel24Filled,
  ShieldKeyhole24Filled,
  Settings24Filled,
  ArrowRight20Regular,
  CheckmarkCircle20Filled,
  Circle20Regular,
  Sparkle20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import {
  useAuditFeed,
  useConnections,
  useStats,
} from "@/lib/hooks";
import { usePrincipal } from "@/lib/hooks";

const useStyles = makeStyles({
  page: { display: "flex", flexDirection: "column", gap: "24px" },

  /* Getting started panel */
  gsCard: {
    backgroundColor: "#ffffff",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "8px",
    padding: "20px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  gsHead: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
  },
  gsBadge: {
    width: "32px",
    height: "32px",
    borderRadius: "6px",
    backgroundColor: "#FBF1EB",
    color: "#B8521B",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
  },
  gsList: { display: "flex", flexDirection: "column", gap: "6px" },
  gsItem: {
    display: "grid",
    gridTemplateColumns: "20px 1fr auto",
    gap: "10px",
    alignItems: "center",
    padding: "8px 0",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  gsItemFirst: { borderTop: "none" },
  gsLabel: { fontSize: "13px", color: tokens.colorNeutralForeground1 },
  gsLabelDone: {
    fontSize: "13px",
    color: tokens.colorNeutralForeground3,
    textDecoration: "line-through",
  },

  /* Stat tiles */
  tileGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))",
    gap: "12px",
  },
  tile: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    textDecoration: "none",
    color: "inherit",
    transitionProperty: "transform, box-shadow",
    transitionDuration: "80ms",
    ":hover": {
      transform: "translateY(-1px)",
      boxShadow: `0 4px 12px rgba(0,0,0,0.06), 0 0 0 1px ${tokens.colorBrandStroke2}`,
    },
  },
  tileHead: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
  },
  tileLabel: {
    color: tokens.colorNeutralForeground3,
    fontSize: "11px",
    letterSpacing: "0.04em",
    textTransform: "uppercase",
    fontWeight: 600,
  },
  tileNumber: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "30px",
    fontWeight: 600,
    color: tokens.colorNeutralForeground1,
    lineHeight: 1.0,
  },
  tileSub: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  tileIcon: { color: tokens.colorBrandForeground1 },

  /* Activity panel */
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
  },
  panelHead: {
    padding: "12px 16px",
    borderBottomWidth: "1px",
    borderBottomStyle: "solid",
    borderBottomColor: tokens.colorNeutralStroke2,
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
  },
  panelLink: {
    display: "inline-flex",
    alignItems: "center",
    gap: "4px",
    color: tokens.colorBrandForeground1,
    fontSize: "12px",
    textDecoration: "none",
  },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
});

interface AuditRow {
  id: string;
  method: string;
  path: string;
  status: number;
  outcome: string;
  when: string;
}

function outcomeColor(o?: string): "success" | "warning" | "danger" | "subtle" {
  if (o === "denied") return "danger";
  if (o === "failure") return "warning";
  if (o === "success") return "success";
  return "subtle";
}

export default function Home() {
  const styles = useStyles();
  const stats = useStats();
  const audit = useAuditFeed(20);
  const conns = useConnections();
  const { principal } = usePrincipal();

  const totalAssets   = stats.data?.catalog?.total ?? 0;
  const taggedAssets  = stats.data?.catalog?.tagged ?? 0;
  const tableCount    = stats.data?.catalog?.by_type?.table ?? 0;
  const columnCount   = stats.data?.catalog?.by_type?.column ?? 0;

  const hasConn       = (stats.data?.connections ?? 0) > 0;
  const hasHealthy    = (stats.data?.healthy_connections ?? 0) > 0;
  const hasAssets     = totalAssets > 0;

  const auditRows = useMemo<AuditRow[]>(() => {
    return (audit.data ?? []).slice(0, 8).map((e: any) => ({
      id: e.event_id,
      method: e.http_method ?? e.action ?? "",
      path: e.http_path ?? e.action ?? "",
      status: e.http_status ?? 0,
      outcome: e.outcome ?? "",
      when: e.created_at,
    }));
  }, [audit.data]);

  const auditCols = useMemo<TableColumnDefinition<AuditRow>[]>(
    () => [
      createTableColumn<AuditRow>({
        columnId: "method",
        renderHeaderCell: () => "Method",
        renderCell: (item) => (
          <span
            className={styles.mono}
            style={{ fontWeight: 600, color: "#B8521B" }}
          >
            {item.method}
          </span>
        ),
      }),
      createTableColumn<AuditRow>({
        columnId: "path",
        renderHeaderCell: () => "Path",
        renderCell: (item) => <span className={styles.mono}>{item.path}</span>,
      }),
      createTableColumn<AuditRow>({
        columnId: "status",
        renderHeaderCell: () => "Status",
        renderCell: (item) => (
          <Badge
            appearance={item.outcome === "success" ? "outline" : "filled"}
            color={outcomeColor(item.outcome)}
          >
            {item.status} {item.outcome}
          </Badge>
        ),
      }),
      createTableColumn<AuditRow>({
        columnId: "when",
        renderHeaderCell: () => "When",
        renderCell: (item) => (
          <span className={styles.meta}>
            {item.when ? new Date(item.when).toLocaleTimeString() : "—"}
          </span>
        ),
      }),
    ],
    [styles],
  );

  const greeting = principal?.fullName?.split(" ")[0] ?? principal?.email?.split("@")[0] ?? "there";

  // Getting-started checklist. Each step has done/icon/cta.
  const steps = [
    {
      label: "Workspace created",
      done: !!principal,
    },
    {
      label: "Email verified",
      done: principal?.verified ?? false,
    },
    {
      label: "Connect a datasource",
      done: hasConn,
      cta: { href: "/connections", label: "Add connection" },
    },
    {
      label: "Crawl your first schema",
      done: hasAssets,
      cta: hasConn ? { href: "/connections", label: "Open Connections" } : undefined,
    },
    {
      label: "Author a quality check (Track 2)",
      done: false,
      disabled: true,
    },
  ];
  const nextStep = steps.find((s) => !s.done && !s.disabled);

  return (
    <div className={styles.page}>
      <PageHeader
        title={`Welcome, ${greeting}`}
        subtitle="Your data context platform — catalog, governance, and AI-native lineage."
        crumbs={[{ label: "Home" }]}
      />

      {/* Getting started */}
      {nextStep && (
        <div className={styles.gsCard}>
          <div className={styles.gsHead}>
            <span className={styles.gsBadge}><Sparkle20Regular /></span>
            <Subtitle1>Get started</Subtitle1>
            <Caption1 style={{ color: tokens.colorNeutralForeground3 }}>
              {steps.filter((s) => s.done).length} of{" "}
              {steps.filter((s) => !s.disabled).length} done
            </Caption1>
          </div>
          <div className={styles.gsList}>
            {steps.map((s, i) => (
              <div
                key={s.label}
                className={`${styles.gsItem} ${i === 0 ? styles.gsItemFirst : ""}`}
              >
                {s.done ? (
                  <CheckmarkCircle20Filled style={{ color: "#107c10" }} />
                ) : (
                  <Circle20Regular style={{ color: tokens.colorNeutralForeground3 }} />
                )}
                <span className={s.done ? styles.gsLabelDone : styles.gsLabel}>
                  {s.label}
                </span>
                {s.cta && !s.done && (
                  <Link href={s.cta.href}>
                    <Button size="small" appearance="primary">
                      {s.cta.label}
                    </Button>
                  </Link>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Stat tiles */}
      <section>
        <Subtitle2 block style={{ marginBottom: 12 }}>Resources</Subtitle2>
        <div className={styles.tileGrid}>
          <Tile
            label="Catalog assets"
            count={totalAssets}
            sub={`${tableCount} tables · ${columnCount} columns`}
            href="/catalog"
            icon={<Database24Filled />}
          />
          <Tile
            label="Connections"
            count={stats.data?.connections ?? 0}
            sub={`${stats.data?.healthy_connections ?? 0} healthy`}
            href="/connections"
            icon={<Settings24Filled />}
          />
          <Tile
            label="Pipelines"
            count={stats.data?.pipelines ?? 0}
            sub="orchestrated jobs"
            href="/pipelines"
            icon={<Flow24Filled />}
          />
          <Tile
            label="Quality checks"
            count={stats.data?.checks ?? 0}
            sub="row count, freshness, custom SQL"
            href="/checks"
            icon={<CheckmarkCircle24Filled />}
          />
          <Tile
            label="Audit events"
            count={audit.data?.length ?? 0}
            sub="hash-chained, append-only"
            href="/admin/audit"
            icon={<Eye24Filled />}
          />
          <Tile
            label="Recycle bin"
            count={stats.data?.deleted_active ?? 0}
            sub="restorable tombstones"
            href="/admin/deleted"
            icon={<Delete24Filled />}
          />
          <Tile
            label="Legal holds"
            count={stats.data?.holds_active ?? 0}
            sub="active litigation gates"
            href="/legal-holds"
            icon={<Gavel24Filled />}
          />
          <Tile
            label="Open DSRs"
            count={stats.data?.dsr_open ?? 0}
            sub="GDPR Art. 15-20"
            href="/dsr"
            icon={<ShieldKeyhole24Filled />}
          />
        </div>
        {taggedAssets > 0 && (
          <Caption1 style={{ color: tokens.colorNeutralForeground3, marginTop: 8 }}>
            <strong>{taggedAssets}</strong> assets carry classifications (PII / PHI / PCI / secret).
          </Caption1>
        )}
      </section>

      {/* Activity */}
      <section>
        <div className={styles.panel}>
          <div className={styles.panelHead}>
            <Subtitle1>Recent activity</Subtitle1>
            <Link href="/admin/audit" className={styles.panelLink}>
              View all <ArrowRight20Regular />
            </Link>
          </div>
          {auditRows.length === 0 ? (
            <div style={{ padding: "24px", textAlign: "center", color: tokens.colorNeutralForeground3 }}>
              No requests yet.
            </div>
          ) : (
            <DataGrid
              items={auditRows}
              columns={auditCols}
              getRowId={(item) => item.id}
              size="small"
            >
              <DataGridHeader>
                <DataGridRow>
                  {({ renderHeaderCell }) => (
                    <DataGridHeaderCell>{renderHeaderCell()}</DataGridHeaderCell>
                  )}
                </DataGridRow>
              </DataGridHeader>
              <DataGridBody<AuditRow>>
                {({ item, rowId }) => (
                  <DataGridRow<AuditRow> key={rowId}>
                    {({ renderCell }) => (
                      <DataGridCell>{renderCell(item)}</DataGridCell>
                    )}
                  </DataGridRow>
                )}
              </DataGridBody>
            </DataGrid>
          )}
        </div>
      </section>
    </div>
  );
}

function Tile({
  label,
  count,
  sub,
  href,
  icon,
}: {
  label: string;
  count: number | string;
  sub?: string;
  href: string;
  icon: React.ReactNode;
}) {
  const styles = useStyles();
  return (
    <Link href={href} className={styles.tile}>
      <div className={styles.tileHead}>
        <Caption1 className={styles.tileLabel}>{label}</Caption1>
        <span className={styles.tileIcon}>{icon}</span>
      </div>
      <div className={styles.tileNumber}>{count}</div>
      {sub && <span className={styles.tileSub}>{sub}</span>}
    </Link>
  );
}
