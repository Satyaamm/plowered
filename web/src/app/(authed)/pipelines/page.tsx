"use client";

import Link from "next/link";
import {
  Button,
  Subtitle2,
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
import { useTriggerPipeline, usePipelines } from "@/lib/hooks";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  header: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "flex-end",
    gap: "16px",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  scheduleCell: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
});

export default function PipelinesPage() {
  const styles = useStyles();
  const { data, isLoading, error } = usePipelines();
  const trigger = useTriggerPipeline();

  return (
    <div className={styles.root}>
      <div className={styles.header}>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <Title2>Pipelines</Title2>
          <Text className={styles.meta}>
            {data?.length ?? 0} pipeline{(data?.length ?? 0) === 1 ? "" : "s"}
          </Text>
        </div>
        <Link href="/pipelines/new">
          <Button appearance="primary">New pipeline</Button>
        </Link>
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorBanner error={error} />}
      {data && data.length === 0 && (
        <EmptyState
          title="No pipelines yet"
          body="Pipelines wire connectors, transforms, and quality checks into a scheduled DAG."
          action={
            <Link href="/pipelines/new">
              <Button appearance="primary">Create your first pipeline</Button>
            </Link>
          }
        />
      )}

      {data && data.length > 0 && (
        <Table aria-label="Pipelines">
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Name</TableHeaderCell>
              <TableHeaderCell>Tasks</TableHeaderCell>
              <TableHeaderCell>Schedule</TableHeaderCell>
              <TableHeaderCell>Updated</TableHeaderCell>
              <TableHeaderCell>Actions</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((p) => (
              <TableRow key={p.ID}>
                <TableCell>
                  <Link
                    href={`/pipelines/${encodeURIComponent(p.ID)}`}
                    style={{ color: tokens.colorBrandForeground1 }}
                  >
                    {p.Name}
                  </Link>
                  {p.Description && (
                    <div className={styles.meta}>{p.Description}</div>
                  )}
                </TableCell>
                <TableCell>{p.Tasks?.length ?? 0}</TableCell>
                <TableCell>
                  {p.Schedule?.Cron ? (
                    <span className={styles.scheduleCell}>
                      {p.Schedule.Cron}
                      {p.Schedule.Enabled ? "" : " (disabled)"}
                    </span>
                  ) : (
                    <span className={styles.meta}>manual</span>
                  )}
                </TableCell>
                <TableCell>
                  <span className={styles.meta}>
                    {new Date(p.UpdatedAt).toLocaleString()}
                  </span>
                </TableCell>
                <TableCell>
                  <Button
                    size="small"
                    onClick={() => trigger.mutate(p.ID)}
                    disabled={trigger.isPending}
                  >
                    {trigger.isPending && trigger.variables === p.ID
                      ? "Triggering…"
                      : "Trigger"}
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {trigger.isSuccess && !trigger.isPending && (
        <Subtitle2>Run queued — see the Runs page for status.</Subtitle2>
      )}
      {trigger.error && <ErrorBanner error={trigger.error} />}
    </div>
  );
}
