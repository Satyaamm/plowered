"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import {
  Button,
  Card,
  CardHeader,
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
import { usePipeline, useTriggerPipeline, useRuns } from "@/lib/hooks";
import { PipelineDAG } from "@/components/pipeline-dag";
import { StatusBadge } from "@/components/status-badge";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  header: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "flex-end",
    gap: "16px",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  metaRow: { display: "flex", gap: "16px", flexWrap: "wrap" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
});

export default function PipelineDetailPage() {
  const styles = useStyles();
  const params = useParams<{ id: string }>();
  const id = params.id;
  const { data: pipeline, isLoading, error } = usePipeline(id);
  const { data: runs } = useRuns({ pipelineId: id, limit: 20 });
  const trigger = useTriggerPipeline();

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorBanner error={error} />;
  if (!pipeline) return <EmptyState title="Pipeline not found" />;

  return (
    <div className={styles.root}>
      <div className={styles.header}>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <Title2>{pipeline.Name}</Title2>
          <div className={styles.metaRow}>
            <Text className={styles.meta}>
              {(pipeline.Tasks ?? []).length} task
              {(pipeline.Tasks ?? []).length === 1 ? "" : "s"}
            </Text>
            {pipeline.Schedule?.Cron && (
              <Text className={`${styles.meta} ${styles.mono}`}>
                {pipeline.Schedule.Cron}
                {pipeline.Schedule.Enabled ? "" : " (disabled)"}
              </Text>
            )}
            {pipeline.FailFast && (
              <Text className={styles.meta}>fail-fast</Text>
            )}
          </div>
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <Link href={`/pipelines/${encodeURIComponent(pipeline.ID)}/edit`}>
            <Button appearance="secondary">Edit DAG</Button>
          </Link>
          <Button
            appearance="primary"
            onClick={() => trigger.mutate(pipeline.ID)}
            disabled={trigger.isPending}
          >
            {trigger.isPending ? "Triggering…" : "Trigger run"}
          </Button>
        </div>
      </div>

      {pipeline.Description && <Text>{pipeline.Description}</Text>}

      <Card>
        <CardHeader header={<Subtitle2>DAG</Subtitle2>} />
        <PipelineDAG tasks={pipeline.Tasks ?? []} />
      </Card>

      <Card>
        <CardHeader header={<Subtitle2>Recent runs</Subtitle2>} />
        {!runs || runs.length === 0 ? (
          <EmptyState
            title="No runs yet"
            body="Trigger the pipeline to see run history here."
          />
        ) : (
          <Table aria-label="Recent runs">
            <TableHeader>
              <TableRow>
                <TableHeaderCell>Run</TableHeaderCell>
                <TableHeaderCell>Status</TableHeaderCell>
                <TableHeaderCell>Triggered by</TableHeaderCell>
                <TableHeaderCell>Started</TableHeaderCell>
                <TableHeaderCell>Finished</TableHeaderCell>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((r) => (
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
                    <StatusBadge variant="run" status={r.Status} />
                  </TableCell>
                  <TableCell>{r.TriggeredBy ?? ""}</TableCell>
                  <TableCell>
                    <span className={styles.meta}>
                      {r.StartedAt ? new Date(r.StartedAt).toLocaleString() : "—"}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className={styles.meta}>
                      {r.FinishedAt ? new Date(r.FinishedAt).toLocaleString() : "—"}
                    </span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </div>
  );
}
