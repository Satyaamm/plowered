"use client";

import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Button,
  Field,
  InfoLabel,
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
import { CronPicker } from "@/components/cron-picker";
import type { Task } from "@/lib/types-orchestration";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "20px" },
  form: {
    display: "grid",
    gridTemplateColumns: "1fr 40px 1fr",
    gap: "0px",
    alignItems: "start",
  },
  divider: {
    width: "1px",
    backgroundColor: tokens.colorNeutralStroke2,
    alignSelf: "stretch",
    justifySelf: "center",
  },
  leftCol: { display: "flex", flexDirection: "column", gap: "16px" },
  rightCol: { display: "flex", flexDirection: "column", gap: "12px" },
  toggleRow: {
    display: "flex",
    gap: "32px",
    alignItems: "center",
    paddingTop: "16px",
    marginTop: "4px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "16px",
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
  const [timezone, setTimezone] = useState("UTC");
  const [enabled, setEnabled] = useState(true);
  const [failFast, setFailFast] = useState(true);
  const [tasks, setTasks] = useState<Task[]>([]);

  const [hydrated, setHydrated] = useState(false);
  useEffect(() => {
    if (pipeline && !hydrated) {
      setName(pipeline.Name ?? "");
      setDescription(pipeline.Description ?? "");
      setCron(pipeline.Schedule?.Cron ?? "");
      setTimezone(pipeline.Schedule?.Timezone || "UTC");
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
      Schedule: cronTrim ? { Cron: cronTrim, Enabled: enabled, Timezone: timezone } : null,
    });
    router.push(`/pipelines/${encodeURIComponent(id)}`);
  };

  return (
    <>
      <PageHeader
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
            <div className={styles.leftCol} style={{ paddingRight: 20 }}>
              <Field
                required
                label={
                  <InfoLabel info="A human-readable identifier for this pipeline. Use kebab-case (e.g. 'nightly-orders'). Must be unique within the workspace; runs are listed under this name in the UI and logs.">
                    Name
                  </InfoLabel>
                }
              >
                <Input value={name} onChange={(_, d) => setName(d.value)} />
              </Field>
              <Field
                label={
                  <InfoLabel info="Optional context shown on the pipeline detail page and in oncall alerts. Note what the pipeline does, who owns it, and who to ping when it fails.">
                    Description
                  </InfoLabel>
                }
              >
                <Textarea
                  rows={4}
                  value={description}
                  onChange={(_, d) => setDescription(d.value)}
                />
              </Field>
            </div>

            <div className={styles.divider} />

            <div className={styles.rightCol} style={{ paddingLeft: 20 }}>
              <CronPicker
                value={{ cron, timezone }}
                onChange={(v) => {
                  setCron(v.cron);
                  setTimezone(v.timezone);
                }}
                required={enabled}
                error={cronError || undefined}
              />
            </div>
          </div>

          <div className={styles.toggleRow}>
            <Switch
              checked={enabled}
              onChange={(_, d) => setEnabled(d.checked)}
              label={
                <InfoLabel info="When enabled, the scheduler fires the pipeline on the cron schedule. Disable to pause without losing history; the pipeline can still be triggered manually or via API.">
                  Schedule enabled
                </InfoLabel>
              }
            />
            <Switch
              checked={failFast}
              onChange={(_, d) => setFailFast(d.checked)}
              label={
                <InfoLabel info="When on, the first failing task aborts the run and downstream tasks are skipped. When off, independent branches keep going so partial completion is recorded.">
                  Fail fast on first task failure
                </InfoLabel>
              }
            />
          </div>
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
