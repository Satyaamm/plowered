"use client";

import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Button,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Switch,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { usePipeline, useUpdatePipeline } from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import { DAGEditor } from "@/components/dag-editor";
import { ErrorBanner, LoadingState } from "@/components/states";
import { InfoLabel } from "@/components/info-label";
import type { Task } from "@/lib/types-orchestration";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "20px" },
  form: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr 200px",
    gap: "16px",
    alignItems: "start",
  },
  toggleCol: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    paddingTop: "26px",
  },
  toggleLabel: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground1,
    whiteSpace: "nowrap",
  },
  toggleRow: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
  },
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
});

function hasCycle(tasks: Task[]): boolean {
  const indeg = new Map<string, number>();
  const out = new Map<string, string[]>();
  for (const t of tasks) {
    indeg.set(t.ID, (t.DependsOn ?? []).length);
    out.set(t.ID, []);
  }
  for (const t of tasks) {
    for (const d of t.DependsOn ?? []) out.get(d)?.push(t.ID);
  }
  let frontier = tasks
    .filter((t) => (indeg.get(t.ID) ?? 0) === 0)
    .map((t) => t.ID);
  let removed = 0;
  while (frontier.length) {
    const next: string[] = [];
    for (const id of frontier) {
      removed++;
      for (const c of out.get(id) ?? []) {
        const r = (indeg.get(c) ?? 0) - 1;
        indeg.set(c, r);
        if (r === 0) next.push(c);
      }
    }
    frontier = next;
  }
  return removed !== tasks.length;
}

export default function EditPipelinePage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const styles = useStyles();
  const router = useRouter();
  const { id } = use(params);
  const { data: pipeline, isLoading, error } = usePipeline(id);
  const update = useUpdatePipeline(id);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [cron, setCron] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [failFast, setFailFast] = useState(true);
  const [tasks, setTasks] = useState<Task[]>([]);

  // Load fields once the pipeline is fetched. Keep the local state
  // canonical from then on so the user's edits don't get clobbered by
  // background refetches.
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => {
    if (pipeline && !hydrated) {
      setName(pipeline.Name ?? "");
      setDescription(pipeline.Description ?? "");
      setCron(pipeline.Schedule?.Cron ?? "");
      setEnabled(pipeline.Schedule?.Enabled ?? true);
      setFailFast(pipeline.FailFast ?? true);
      setTasks((pipeline.Tasks ?? []) as Task[]);
      setHydrated(true);
    }
  }, [pipeline, hydrated]);

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorBanner error={error} />;
  if (!pipeline) return null;

  const cycle = hasCycle(tasks);
  const CRON_RE = /^(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)$/;
  const cronTrim = cron.trim();
  const cronError =
    enabled && !cronTrim
      ? "Cron expression is required when a schedule is enabled"
      : cronTrim && !CRON_RE.test(cronTrim)
        ? "Expected 5 fields, e.g. '0 3 * * *'"
        : "";
  const valid = name && !cycle && !cronError;

  const submit = async () => {
    if (!valid) return;
    await update.mutateAsync({
      Name: name,
      Description: description,
      FailFast: failFast,
      Tasks: tasks,
      Schedule: cronTrim ? { Cron: cronTrim, Enabled: enabled, Timezone: "UTC" } : null,
    });
    router.push(`/pipelines/${encodeURIComponent(id)}`);
  };

  return (
    <>
      <PageHeader
        title={`Edit ${pipeline.Name}`}
        subtitle="Pipelines are versioned implicitly via updated_at — saving overwrites the active definition."
        crumbs={[
          { label: "Home", href: "/" },
          { label: "Pipelines", href: "/pipelines" },
          { label: pipeline.Name, href: `/pipelines/${id}` },
          { label: "Edit" },
        ]}
        actions={
          <>
            <Button onClick={() => router.back()}>Cancel</Button>
            <Button
              appearance="primary"
              onClick={submit}
              disabled={!valid || update.isPending}
            >
              {update.isPending ? "Saving…" : "Save"}
            </Button>
          </>
        }
      />

      <div className={styles.body}>
        <div className={styles.panel}>
          <div className={styles.form}>
            <Field
              label={
                <InfoLabel info="A human-readable identifier for this pipeline. Use kebab-case (e.g. 'nightly-orders'). Must be unique within the workspace.">
                  Name
                </InfoLabel>
              }
              required
            >
              <Input value={name} onChange={(_, d) => setName(d.value)} />
            </Field>
            <Field
              label={
                <InfoLabel info="Five-field cron expression (minute hour day-of-month month day-of-week). Examples: '0 3 * * *' = daily at 03:00; '0 9 * * 1-5' = weekdays 09:00; '*/15 * * * *' = every 15 minutes.">
                  {enabled ? "Cron expression" : "Cron (optional)"}
                </InfoLabel>
              }
              required={enabled}
              validationState={cronError ? "error" : "none"}
              validationMessage={cronError || undefined}
            >
              <Input
                value={cron}
                onChange={(_, d) => setCron(d.value)}
                placeholder="0 3 * * *"
                style={{ fontFamily: "ui-monospace, monospace" }}
              />
            </Field>
            <div className={styles.toggleCol}>
              <div className={styles.toggleRow}>
                <Switch
                  checked={enabled}
                  onChange={(_, d) => setEnabled(d.checked)}
                />
                <span className={styles.toggleLabel}>
                  <InfoLabel info="When on, the scheduler fires the pipeline on the cron above. Off pauses without losing history.">
                    Schedule enabled
                  </InfoLabel>
                </span>
              </div>
              <div className={styles.toggleRow}>
                <Switch
                  checked={failFast}
                  onChange={(_, d) => setFailFast(d.checked)}
                />
                <span className={styles.toggleLabel}>
                  <InfoLabel info="When on, the first failing task aborts the run and downstream tasks are skipped. Off lets independent branches keep going.">
                    Fail fast
                  </InfoLabel>
                </span>
              </div>
            </div>
          </div>
          <Field
            label={
              <InfoLabel info="Optional context shown on the pipeline detail page and in oncall alerts.">
                Description
              </InfoLabel>
            }
          >
            <Textarea
              rows={2}
              value={description}
              onChange={(_, d) => setDescription(d.value)}
            />
          </Field>
        </div>

        {cycle && (
          <MessageBar intent="error">
            <MessageBarBody>
              Cycle in the DAG. Resolve before saving.
            </MessageBarBody>
          </MessageBar>
        )}

        <DAGEditor tasks={tasks} onChange={setTasks} />

        {update.error && <ErrorBanner error={update.error} />}
      </div>
    </>
  );
}
