"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
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
import { useCreatePipeline } from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import { ErrorBanner } from "@/components/states";
import { DAGEditor } from "@/components/dag-editor";
import { InfoLabel } from "@/components/info-label";
import type { Task } from "@/lib/types-orchestration";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "20px" },
  form: {
    display: "grid",
    // Two equal data columns + a fixed toggle rail. alignItems:start so
    // the Cron validation message pushing one row taller doesn't drag
    // the other columns out of line.
    gridTemplateColumns: "1fr 1fr 200px",
    gap: "16px",
    alignItems: "start",
  },
  toggleCol: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    // Align with the *input* in the sibling Fields, not the field label
    // (Fluent v9 Field label is ~20px tall + 4px gap to the input).
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

export default function NewPipelinePage() {
  const styles = useStyles();
  const router = useRouter();
  const create = useCreatePipeline();

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [cron, setCron] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [failFast, setFailFast] = useState(true);
  const [tasks, setTasks] = useState<Task[]>([]);

  const hasCycle = (() => {
    // Quick re-check before submit: BFS-style topo. Cheap.
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

  // Cron is a standard 5-field expression (minute hour day month weekday).
  // We accept *, */N, comma lists and ranges in each slot.
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
      Schedule: cronTrim ? { Cron: cronTrim, Enabled: enabled, Timezone: "UTC" } : null,
    });
    router.push(`/pipelines/${encodeURIComponent(created.ID)}`);
  };

  return (
    <>
      <PageHeader
        title="New pipeline"
        subtitle="Wire tasks into a DAG. Each task runs when its dependencies finish."
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
            <Field
              label={
                <InfoLabel info="A human-readable identifier for this pipeline. Use kebab-case (e.g. 'nightly-orders'). Must be unique within the workspace; runs are listed under this name in the UI and logs.">
                  Name
                </InfoLabel>
              }
              required
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
                <InfoLabel info="Five-field cron expression (minute hour day-of-month month day-of-week). Examples: '0 3 * * *' = daily at 03:00; '0 9 * * 1-5' = weekdays 09:00; '*/15 * * * *' = every 15 minutes. Evaluated in the workspace timezone.">
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
                  <InfoLabel info="When on, the scheduler fires the pipeline on the cron above. Off pauses the schedule without losing history — you can still trigger it manually or via API.">
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
                  <InfoLabel info="When on, the first failing task aborts the run and downstream tasks are skipped. When off, independent branches keep going so partial completion is recorded.">
                    Fail fast
                  </InfoLabel>
                </span>
              </div>
            </div>
          </div>
          <Field
            label={
              <InfoLabel info="Optional context shown on the pipeline detail page and in oncall alerts. Note what the pipeline does, who owns it, and who to ping when it fails.">
                Description
              </InfoLabel>
            }
          >
            <Textarea
              rows={2}
              value={description}
              onChange={(_, d) => setDescription(d.value)}
              placeholder="What this pipeline does and who to ping when it fails."
            />
          </Field>
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
