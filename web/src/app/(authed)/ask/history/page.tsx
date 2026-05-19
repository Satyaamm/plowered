"use client";

import Link from "next/link";
import { useState } from "react";
import {
  Badge,
  Button,
  Caption1,
  Card,
  Spinner,
  Subtitle2,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  ArrowLeft20Regular,
  Play20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  AskHistoryEntry,
  useAskHistory,
  useAskRun,
  useConnections,
} from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "12px" },
  row: {
    padding: "12px 14px",
    display: "flex",
    flexDirection: "column",
    gap: "6px",
    cursor: "pointer",
  },
  rowHead: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    flexWrap: "wrap",
  },
  question: { fontSize: "13px", fontWeight: 600 },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  sqlBox: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    backgroundColor: tokens.colorNeutralBackground3,
    padding: "10px 12px",
    borderRadius: "4px",
    whiteSpace: "pre-wrap",
    overflowX: "auto",
    color: tokens.colorNeutralForeground1,
  },
  flow: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
  },
});

export default function AskHistoryPage() {
  const styles = useStyles();
  const history = useAskHistory(200);

  return (
    <div className={styles.root}>
      <PageHeader
        title="Ask history"
        subtitle="Every question asked through /ask. Click a row to expand the generated SQL, re-run, or copy."
        crumbs={[
          { label: "Home", href: "/" },
          { label: "Ask", href: "/ask" },
          { label: "History" },
        ]}
        actions={
          <Link href="/ask">
            <Button icon={<ArrowLeft20Regular />}>Back to Ask</Button>
          </Link>
        }
      />

      {history.isLoading && <LoadingState />}
      {history.error && <ErrorBanner error={history.error as Error} />}
      {history.data && history.data.length === 0 && (
        <EmptyState
          title="No questions asked yet"
          body="Open the Ask page and try a question. Every generation lands here for replay."
        />
      )}
      {history.data?.map((e) => <HistoryRow key={e.ID} entry={e} />)}
    </div>
  );
}

function HistoryRow({ entry }: { entry: AskHistoryEntry }) {
  const styles = useStyles();
  const [expanded, setExpanded] = useState(false);
  const conns = useConnections();
  const run = useAskRun(entry.ID);

  const connName =
    conns.data?.find((c) => c.id === entry.ConnectionID)?.name ?? entry.ConnectionID.slice(0, 8);

  return (
    <Card className={styles.row} onClick={() => setExpanded((x) => !x)}>
      <div className={styles.rowHead}>
        <StatusBadge status={entry.Status} />
        <span className={styles.question}>{entry.Question}</span>
        <span style={{ flex: 1 }} />
        <Badge appearance="tint" color="informative">{connName}</Badge>
        <Caption1 className={styles.meta}>
          {new Date(entry.GeneratedAt).toLocaleString()}
        </Caption1>
      </div>
      <div className={styles.flow}>
        <Caption1 className={styles.meta}>{entry.Model}</Caption1>
        {entry.RowCount !== undefined && entry.RowCount !== null && (
          <Caption1 className={styles.meta}>
            · {entry.RowCount.toLocaleString()} rows
          </Caption1>
        )}
        {entry.ElapsedMs !== undefined && entry.ElapsedMs !== null && (
          <Caption1 className={styles.meta}>· {entry.ElapsedMs} ms</Caption1>
        )}
        {entry.Error && (
          <Caption1 style={{ color: tokens.colorPaletteRedForeground1, fontSize: 12 }}>
            · {truncate(entry.Error, 100)}
          </Caption1>
        )}
      </div>
      {expanded && (
        <div onClick={(e) => e.stopPropagation()}>
          <pre className={styles.sqlBox}>{entry.GeneratedSQL}</pre>
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 8 }}>
            <Button
              size="small"
              appearance="primary"
              icon={run.isPending ? <Spinner size="extra-tiny" /> : <Play20Regular />}
              onClick={() => run.mutate()}
              disabled={run.isPending}
            >
              {run.isPending ? "Running…" : "Run again"}
            </Button>
          </div>
          {run.data && (
            <Caption1 className={styles.meta} style={{ marginTop: 6 }}>
              Returned {run.data.row_count} row{run.data.row_count === 1 ? "" : "s"} in {run.data.elapsed_ms} ms
              {run.data.truncated && " (truncated)"}
            </Caption1>
          )}
          {run.error && (
            <Text style={{ color: tokens.colorPaletteRedForeground1, fontSize: 12 }}>
              {(run.error as Error).message}
            </Text>
          )}
        </div>
      )}
    </Card>
  );
}

function StatusBadge({ status }: { status: AskHistoryEntry["Status"] }) {
  const color =
    status === "executed" ? "success"
    : status === "failed" ? "danger"
    : "subtle";
  return (
    <Badge appearance="tint" color={color}>{status}</Badge>
  );
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}
