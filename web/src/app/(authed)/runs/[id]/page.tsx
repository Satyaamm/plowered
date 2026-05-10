"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  Badge,
  Body1,
  Button,
  Card,
  CardHeader,
  Subtitle2,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  Title2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { useRun, useRunLogs, useTaskRuns } from "@/lib/hooks";
import { StatusBadge } from "@/components/status-badge";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  metaRow: { display: "flex", gap: "16px", flexWrap: "wrap", alignItems: "center" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
  errorCell: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorPaletteRedForeground1,
    whiteSpace: "pre-wrap",
  },
  output: {
    backgroundColor: tokens.colorNeutralBackground2,
    borderRadius: "4px",
    padding: "6px 8px",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "11px",
    overflowX: "auto",
    maxWidth: "100%",
  },
  logsHeader: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    width: "100%",
  },
  logsBody: {
    backgroundColor: "#0E1116",
    color: "#D7DCE2",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    lineHeight: "1.5",
    padding: "12px 14px",
    borderRadius: "6px",
    height: "360px",
    overflowY: "auto",
    whiteSpace: "pre-wrap",
    wordBreak: "break-word",
  },
  logRow: { display: "flex", gap: "10px" },
  logTs: { color: "#6E7787", flexShrink: 0, width: "84px" },
  logTaskTag: {
    color: "#7CB7FF",
    flexShrink: 0,
    width: "120px",
    overflow: "hidden",
    textOverflow: "ellipsis",
  },
  logLevelInfo: { color: "#D7DCE2" },
  logLevelWarn: { color: "#E8C547" },
  logLevelError: { color: "#FF8A8A" },
});

export default function RunDetailPage() {
  const styles = useStyles();
  const params = useParams<{ id: string }>();
  const id = params.id;
  const { data: run, isLoading, error } = useRun(id);
  const { data: tasks } = useTaskRuns(id);
  const logs = useRunLogs(id);

  const [autoScroll, setAutoScroll] = useState(true);
  const logsEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: "smooth", block: "end" });
    }
  }, [logs.lines.length, autoScroll]);

  const elapsedMs = useMemo(() => {
    if (!run?.StartedAt) return null;
    const end = run.FinishedAt ? new Date(run.FinishedAt).getTime() : Date.now();
    return end - new Date(run.StartedAt).getTime();
  }, [run?.StartedAt, run?.FinishedAt]);

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorBanner error={error} />;
  if (!run) return <EmptyState title="Run not found" />;

  return (
    <div className={styles.root}>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <Title2>Run {run.ID.slice(0, 8)}</Title2>
        <div className={styles.metaRow}>
          <StatusBadge variant="run" status={run.Status} />
          <Text className={styles.meta}>
            Pipeline:&nbsp;
            <Link
              href={`/pipelines/${encodeURIComponent(run.PipelineID)}`}
              className={styles.mono}
              style={{ color: tokens.colorBrandForeground1 }}
            >
              {run.PipelineID.slice(0, 8)}
            </Link>
          </Text>
          {run.TriggeredBy && (
            <Text className={styles.meta}>Triggered by: {run.TriggeredBy}</Text>
          )}
          {run.StartedAt && (
            <Text className={styles.meta}>
              Started: {new Date(run.StartedAt).toLocaleString()}
            </Text>
          )}
          {run.FinishedAt && (
            <Text className={styles.meta}>
              Finished: {new Date(run.FinishedAt).toLocaleString()}
            </Text>
          )}
          {elapsedMs !== null && (
            <Text className={styles.meta}>{formatElapsed(elapsedMs)}</Text>
          )}
        </div>
      </div>

      <Card>
        <CardHeader header={<Subtitle2>Tasks</Subtitle2>} />
        {!tasks || tasks.length === 0 ? (
          <Body1>No task runs recorded yet.</Body1>
        ) : (
          <Table aria-label="Task runs">
            <TableHeader>
              <TableRow>
                <TableHeaderCell>Task</TableHeaderCell>
                <TableHeaderCell>Status</TableHeaderCell>
                <TableHeaderCell>Attempts</TableHeaderCell>
                <TableHeaderCell>Duration</TableHeaderCell>
                <TableHeaderCell>Output</TableHeaderCell>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tasks.map((t) => (
                <TableRow key={t.ID}>
                  <TableCell className={styles.mono}>{t.TaskID}</TableCell>
                  <TableCell>
                    <StatusBadge variant="task" status={t.Status} />
                    {t.DeadLetter && (
                      <Badge appearance="filled" color="danger" style={{ marginLeft: 6 }}>
                        DLQ
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>{t.AttemptCount}</TableCell>
                  <TableCell>
                    <Text className={styles.meta}>{taskDuration(t)}</Text>
                  </TableCell>
                  <TableCell>
                    {t.Error ? (
                      <span className={styles.errorCell}>{t.Error}</span>
                    ) : t.Output && Object.keys(t.Output).length > 0 ? (
                      <pre className={styles.output}>
                        {formatOutput(t.Output)}
                      </pre>
                    ) : (
                      <Text className={styles.meta}>—</Text>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <Card>
        <CardHeader
          header={<Subtitle2>Live logs</Subtitle2>}
          action={
            <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
              <Text className={styles.meta}>
                {streamLabel(logs.status)}
                {logs.lines.length > 0 && ` · ${logs.lines.length} lines`}
              </Text>
              <Switch
                label="Auto-scroll"
                checked={autoScroll}
                onChange={(_, d) => setAutoScroll(d.checked)}
              />
              <Button
                size="small"
                appearance="subtle"
                onClick={() => navigator.clipboard.writeText(logs.lines.map((l) => l.line).join("\n"))}
                disabled={logs.lines.length === 0}
              >
                Copy
              </Button>
            </div>
          }
        />
        <div className={styles.logsBody}>
          {logs.lines.length === 0 ? (
            <Text style={{ color: "#6E7787" }}>
              {logs.status === "open" ? "Waiting for output…" : "No logs yet."}
            </Text>
          ) : (
            logs.lines.map((l) => (
              <div key={l.id} className={styles.logRow}>
                <span className={styles.logTs}>
                  {new Date(l.created_at).toLocaleTimeString(undefined, { hour12: false })}
                </span>
                <span className={styles.logTaskTag}>{l.task_id ?? "run"}</span>
                <span className={levelClass(l.level, styles)}>{l.line}</span>
              </div>
            ))
          )}
          <div ref={logsEndRef} />
        </div>
      </Card>
    </div>
  );
}

function streamLabel(status: ReturnType<typeof useRunLogs>["status"]): string {
  switch (status) {
    case "open":
      return "● streaming";
    case "done":
      return "✓ run complete";
    case "error":
      return "✕ stream closed";
    default:
      return "";
  }
}

function taskDuration(t: { StartedAt?: string; FinishedAt?: string }): string {
  if (!t.StartedAt) return "—";
  const start = new Date(t.StartedAt).getTime();
  const end = t.FinishedAt ? new Date(t.FinishedAt).getTime() : Date.now();
  return formatElapsed(end - start);
}

function formatElapsed(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rs = s % 60;
  return `${m}m ${rs}s`;
}

function formatOutput(out: Record<string, unknown>): string {
  return Object.entries(out)
    .map(([k, v]) => `${k}: ${typeof v === "object" ? JSON.stringify(v) : String(v)}`)
    .join("\n");
}

function levelClass(level: string, styles: ReturnType<typeof useStyles>): string {
  if (level === "warn") return styles.logLevelWarn;
  if (level === "error") return styles.logLevelError;
  return styles.logLevelInfo;
}
