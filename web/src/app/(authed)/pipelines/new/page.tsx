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
import type { Task } from "@/lib/types-orchestration";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "20px" },
  form: { display: "grid", gridTemplateColumns: "1fr 1fr 220px", gap: "16px" },
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

  const valid = name && !hasCycle;

  const submit = async () => {
    if (!valid) return;
    const created = await create.mutateAsync({
      Name: name,
      Description: description,
      FailFast: failFast,
      Concurrency: 4,
      Tasks: tasks,
      Schedule: cron ? { Cron: cron, Enabled: enabled, Timezone: "UTC" } : null,
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
            <Field label="Name" required>
              <Input
                value={name}
                onChange={(_, d) => setName(d.value)}
                placeholder="nightly-orders"
                autoFocus
              />
            </Field>
            <Field label="Cron (optional)">
              <Input
                value={cron}
                onChange={(_, d) => setCron(d.value)}
                placeholder="0 3 * * *"
                style={{ fontFamily: "ui-monospace, monospace" }}
              />
            </Field>
            <div style={{ display: "flex", flexDirection: "column", gap: 4, justifyContent: "center" }}>
              <Switch
                label="Schedule enabled"
                checked={enabled}
                onChange={(_, d) => setEnabled(d.checked)}
              />
              <Switch
                label="Fail fast on first task failure"
                checked={failFast}
                onChange={(_, d) => setFailFast(d.checked)}
              />
            </div>
          </div>
          <Field label="Description">
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
