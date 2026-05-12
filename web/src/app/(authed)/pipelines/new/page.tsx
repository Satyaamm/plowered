"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
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
import { useCreatePipeline } from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import { ErrorBanner } from "@/components/states";
import { DAGEditor } from "@/components/dag-editor";
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

export default function NewPipelinePage() {
  const styles = useStyles();
  const router = useRouter();
  const create = useCreatePipeline();

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [cron, setCron] = useState("");
  const [timezone, setTimezone] = useState("UTC");
  const [enabled, setEnabled] = useState(true);
  const [failFast, setFailFast] = useState(true);
  const [tasks, setTasks] = useState<Task[]>([]);

  const hasCycle = (() => {
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
  })();

  const CRON_RE = /^(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)$/;
  const cronTrim = cron.trim();
  const cronError =
    enabled && !cronTrim
      ? "Cron expression is required when a schedule is enabled"
      : cronTrim && !CRON_RE.test(cronTrim)
        ? "Expected 5 fields, e.g. '0 3 * * *'"
        : "";
  const valid = name && !hasCycle && !cronError;

  const submit = async () => {
    if (!valid) return;
    const created = await create.mutateAsync({
      Name: name,
      Description: description,
      FailFast: failFast,
      Concurrency: 4,
      Tasks: tasks,
      Schedule: cronTrim ? { Cron: cronTrim, Enabled: enabled, Timezone: timezone } : null,
    });
    router.push(`/pipelines/${encodeURIComponent(created.ID)}`);
  };

  return (
    <>
      <PageHeader
        crumbs={[
          { label: "Home", href: "/" },
          { label: "Pipelines", href: "/pipelines" },
          { label: "New" },
        ]}
        actions={
          <>
            <Button onClick={() => router.back()}>Cancel</Button>
            <Button
              appearance="primary"
              onClick={submit}
              disabled={!valid || create.isPending}
            >
              {create.isPending ? "Creating…" : "Create pipeline"}
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
                <Input
                  value={name}
                  onChange={(_, d) => setName(d.value)}
                  placeholder="nightly-orders"
                  autoFocus
                />
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
                  placeholder="What this pipeline does and who to ping when it fails."
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

        {hasCycle && (
          <MessageBar intent="error">
            <MessageBarBody>
              The DAG contains a cycle. Resolve it before saving — the runner refuses cyclic pipelines.
            </MessageBarBody>
          </MessageBar>
        )}

        <DAGEditor tasks={tasks} onChange={setTasks} />

        {create.error && <ErrorBanner error={create.error} />}
      </div>
    </>
  );
}
