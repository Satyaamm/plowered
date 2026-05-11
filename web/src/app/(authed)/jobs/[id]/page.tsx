"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import {
  Badge,
  Body1,
  Caption1,
  Card,
  ProgressBar,
  Spinner,
  Subtitle2,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { PageHeader } from "@/components/page-header";
import { ErrorBanner } from "@/components/states";
import { useJob } from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  card: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "20px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  rowGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
    gap: "12px",
  },
  cell: { display: "flex", flexDirection: "column", gap: "2px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  pre: {
    backgroundColor: tokens.colorNeutralBackground2,
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: "4px",
    padding: "12px",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    overflow: "auto",
  },
});

const STATUS_COLOR: Record<string, "informative" | "success" | "danger" | "warning" | "brand"> =
  {
    queued: "informative",
    running: "brand",
    succeeded: "success",
    failed: "danger",
    cancelled: "warning",
  };

export default function JobDetailPage() {
  const styles = useStyles();
  const { id } = useParams<{ id: string }>();
  const job = useJob(id);

  if (job.isLoading) {
    return (
      <div style={{ padding: 24 }}>
        <Spinner label="Loading job…" />
      </div>
    );
  }
  if (job.error) return <ErrorBanner error={job.error as Error} />;
  if (!job.data) {
    return (
      <Body1>
        Job not found. <Link href="/runs">Back to runs.</Link>
      </Body1>
    );
  }

  const j = job.data;
  const isLive = j.status === "queued" || j.status === "running";

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Orchestration", href: "/runs" }, { label: "Job" }]}
        title={j.type}
        subtitle={`Job ${j.id}`}
        actions={
          <Badge appearance="filled" color={STATUS_COLOR[j.status] ?? "subtle"}>
            {j.status}
          </Badge>
        }
      />

      {isLive && (
        <Card className={styles.card}>
          <Subtitle2>Progress</Subtitle2>
          <ProgressBar value={j.progress_pct / 100} />
          <Caption1 className={styles.meta}>
            {j.progress_pct}%{j.message ? ` — ${j.message}` : ""}
          </Caption1>
        </Card>
      )}

      <Card className={styles.card}>
        <Subtitle2>Details</Subtitle2>
        <div className={styles.rowGrid}>
          <div className={styles.cell}>
            <Caption1 className={styles.meta}>Resource</Caption1>
            <Text>{j.resource_id || "—"}</Text>
          </div>
          <div className={styles.cell}>
            <Caption1 className={styles.meta}>Triggered by</Caption1>
            <Text>{j.actor_id || "system"}</Text>
          </div>
          <div className={styles.cell}>
            <Caption1 className={styles.meta}>Created</Caption1>
            <Text>{new Date(j.created_at).toLocaleString()}</Text>
          </div>
          {j.started_at && (
            <div className={styles.cell}>
              <Caption1 className={styles.meta}>Started</Caption1>
              <Text>{new Date(j.started_at).toLocaleString()}</Text>
            </div>
          )}
          {j.finished_at && (
            <div className={styles.cell}>
              <Caption1 className={styles.meta}>Finished</Caption1>
              <Text>{new Date(j.finished_at).toLocaleString()}</Text>
            </div>
          )}
        </div>
      </Card>

      {j.status === "failed" && j.error && (
        <Card className={styles.card}>
          <Subtitle2>Error</Subtitle2>
          <pre className={styles.pre}>{j.error}</pre>
        </Card>
      )}

      {j.status === "succeeded" && j.result && Object.keys(j.result).length > 0 && (
        <Card className={styles.card}>
          <Subtitle2>Result</Subtitle2>
          <pre className={styles.pre}>{JSON.stringify(j.result, null, 2)}</pre>
        </Card>
      )}
    </div>
  );
}
