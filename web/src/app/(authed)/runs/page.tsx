"use client";

import Link from "next/link";
import {
  Body1,
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
import { useRuns } from "@/lib/hooks";
import { StatusBadge } from "@/components/status-badge";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
});

function durationLabel(start?: string, end?: string): string {
  if (!start) return "—";
  const startMs = new Date(start).getTime();
  const endMs = end ? new Date(end).getTime() : Date.now();
  const seconds = Math.max(0, Math.round((endMs - startMs) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  return `${Math.round(minutes / 60)}h`;
}

export default function RunsPage() {
  const styles = useStyles();
  const { data, isLoading, error } = useRuns({ limit: 100 });

  return (
    <div className={styles.root}>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <Title2>Runs</Title2>
        <Body1>
          Most recent pipeline runs across every pipeline. Polls every 5
          seconds while runs are in flight.
        </Body1>
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorBanner error={error} />}
      {data && data.length === 0 && (
        <EmptyState
          title="No runs yet"
          body="Trigger a pipeline to see it appear here."
        />
      )}

      {data && data.length > 0 && (
        <Table aria-label="Recent runs">
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Run</TableHeaderCell>
              <TableHeaderCell>Pipeline</TableHeaderCell>
              <TableHeaderCell>Status</TableHeaderCell>
              <TableHeaderCell>Triggered by</TableHeaderCell>
              <TableHeaderCell>Started</TableHeaderCell>
              <TableHeaderCell>Duration</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((r) => (
              <TableRow key={r.ID}>
                <TableCell>
                  <Link
                    href={`/runs/${encodeURIComponent(r.ID)}`}
                    style={{ color: tokens.colorBrandForeground1 }}
                    className={styles.mono}
                  >
                    {r.ID.slice(0, 8)}
                  </Link>
                </TableCell>
                <TableCell>
                  <Link
                    href={`/pipelines/${encodeURIComponent(r.PipelineID)}`}
                    style={{ color: tokens.colorBrandForeground1 }}
                    className={styles.mono}
                  >
                    {r.PipelineID.slice(0, 8)}
                  </Link>
                </TableCell>
                <TableCell>
                  <StatusBadge variant="run" status={r.Status} />
                </TableCell>
                <TableCell>
                  <Text className={styles.meta}>{r.TriggeredBy ?? ""}</Text>
                </TableCell>
                <TableCell>
                  <Text className={styles.meta}>
                    {r.StartedAt
                      ? new Date(r.StartedAt).toLocaleString()
                      : new Date(r.ScheduledAt).toLocaleString()}
                  </Text>
                </TableCell>
                <TableCell>
                  <Text className={styles.meta}>
                    {durationLabel(r.StartedAt, r.FinishedAt)}
                  </Text>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
