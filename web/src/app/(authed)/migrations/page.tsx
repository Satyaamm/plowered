"use client";

import { useMemo, useState } from "react";
import {
  Badge,
  Button,
  Caption1,
  Card,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Dropdown,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Subtitle1,
  Subtitle2,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  Delete20Regular,
  Play20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { InfoLabel } from "@/components/info-label";
import {
  CreateMigrationPlanInput,
  MigrationColumnMap,
  MigrationPlan,
  MigrationRun,
  useConnections,
  useCreateMigrationPlan,
  useDeleteMigrationPlan,
  useMigrationPlans,
  useMigrationRuns,
  useRunMigrationPlan,
} from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  planCard: {
    padding: "14px 16px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
  },
  planHeader: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    flexWrap: "wrap",
  },
  flow: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorNeutralForeground2,
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  runRow: {
    display: "grid",
    gridTemplateColumns: "100px 1fr 1fr 1fr",
    alignItems: "center",
    gap: "8px",
    padding: "6px 0",
    fontSize: "12px",
  },
  formStack: { display: "flex", flexDirection: "column", gap: "10px", minWidth: "520px" },
  twoCol: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: "10px" },
});

export default function MigrationsPage() {
  const styles = useStyles();
  const plans = useMigrationPlans();
  const [open, setOpen] = useState(false);

  return (
    <div className={styles.root}>
      <PageHeader
        title="Migrations"
        subtitle="Move data between connected sources. Snapshot mode copies the source table to the destination on every run."
        crumbs={[{ label: "Home", href: "/" }, { label: "Migrations" }]}
        actions={
          <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
            <Button
              appearance="primary"
              icon={<Add20Regular />}
              onClick={() => setOpen(true)}
            >
              New plan
            </Button>
            <CreatePlanDialog onClose={() => setOpen(false)} />
          </Dialog>
        }
      />

      {plans.isLoading && <LoadingState />}
      {plans.error && <ErrorBanner error={plans.error as Error} />}
      {plans.data && plans.data.length === 0 && (
        <EmptyState
          title="No migration plans yet"
          body="Create a plan to copy a table from one connected source to another. Snapshot mode is supported today; incremental + CDC are on the roadmap."
        />
      )}
      {plans.data?.map((p) => (
        <PlanCard key={p.id} plan={p} />
      ))}
    </div>
  );
}

function PlanCard({ plan }: { plan: MigrationPlan }) {
  const styles = useStyles();
  const runs = useMigrationRuns(plan.id);
  const run = useRunMigrationPlan();
  const del = useDeleteMigrationPlan();
  const latest = runs.data?.[0];

  return (
    <Card className={styles.planCard}>
      <div className={styles.planHeader}>
        <Subtitle1>{plan.name}</Subtitle1>
        <Badge appearance="tint" color="brand">{plan.mode}</Badge>
        <Badge appearance="tint" color="subtle">{plan.write_mode}</Badge>
        <span style={{ flex: 1 }} />
        {latest && <StatusBadge status={latest.status} />}
        <Button
          appearance="primary"
          size="small"
          icon={
            run.isPending ? <Spinner size="extra-tiny" /> : <Play20Regular />
          }
          onClick={() => run.mutate(plan.id)}
          disabled={run.isPending}
        >
          {run.isPending ? "Running…" : "Run now"}
        </Button>
        <Button
          appearance="subtle"
          size="small"
          icon={<Delete20Regular />}
          aria-label="Delete plan"
          onClick={() => {
            if (confirm(`Delete plan "${plan.name}"? Runs history is removed too.`)) {
              del.mutate(plan.id);
            }
          }}
        />
      </div>
      <div className={styles.flow}>
        {`${qual(plan.source_schema, plan.source_table)} → ${qual(plan.dest_schema, plan.dest_table)}`}
      </div>
      <Caption1 className={styles.meta}>
        {plan.column_map.length} column{plan.column_map.length === 1 ? "" : "s"} mapped
      </Caption1>
      {run.error && (
        <MessageBar intent="error">
          <MessageBarBody>{(run.error as Error).message}</MessageBarBody>
        </MessageBar>
      )}
      {runs.data && runs.data.length > 0 && (
        <RunsList runs={runs.data.slice(0, 5)} />
      )}
    </Card>
  );
}

function RunsList({ runs }: { runs: MigrationRun[] }) {
  const styles = useStyles();
  return (
    <div>
      <Subtitle2 style={{ fontSize: "12px", textTransform: "uppercase", letterSpacing: "0.04em" }}>
        Recent runs
      </Subtitle2>
      {runs.map((r) => (
        <div key={r.id} className={styles.runRow}>
          <StatusBadge status={r.status} />
          <span>{new Date(r.started_at).toLocaleString()}</span>
          <span className={styles.meta}>
            {r.rows_read.toLocaleString()} → {r.rows_written.toLocaleString()} rows
          </span>
          <span className={styles.meta}>
            {r.finished_at
              ? `${msBetween(r.started_at, r.finished_at)} ms`
              : "running…"}
            {r.error && ` · ${truncate(r.error, 80)}`}
          </span>
        </div>
      ))}
    </div>
  );
}

function StatusBadge({ status }: { status: MigrationRun["status"] }) {
  const color =
    status === "succeeded" ? "success"
    : status === "failed" ? "danger"
    : "warning";
  return (
    <Badge appearance="filled" color={color}>{status}</Badge>
  );
}

function CreatePlanDialog({ onClose }: { onClose: () => void }) {
  const styles = useStyles();
  const conns = useConnections();
  const create = useCreateMigrationPlan();

  const sqlConns = (conns.data ?? []).filter((c) =>
    ["postgres", "snowflake", "mysql", "redshift", "bigquery", "athena", "databricks"]
      .includes(c.type),
  );

  const [name, setName] = useState("");
  const [srcConn, setSrcConn] = useState("");
  const [srcSchema, setSrcSchema] = useState("");
  const [srcTable, setSrcTable] = useState("");
  const [dstConn, setDstConn] = useState("");
  const [dstSchema, setDstSchema] = useState("");
  const [dstTable, setDstTable] = useState("");
  const [columnsRaw, setColumnsRaw] = useState("");
  const [writeMode, setWriteMode] = useState<"truncate_and_replace" | "append">("truncate_and_replace");

  const colMap: MigrationColumnMap[] = useMemo(() => {
    return columnsRaw
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
      .map((col) => ({ source_col: col, dest_col: col }));
  }, [columnsRaw]);

  const valid =
    name && srcConn && srcTable && dstConn && dstTable && colMap.length > 0;

  const submit = async () => {
    const body: CreateMigrationPlanInput = {
      name,
      source_connection_id: srcConn,
      source_schema: srcSchema,
      source_table: srcTable,
      dest_connection_id: dstConn,
      dest_schema: dstSchema,
      dest_table: dstTable,
      column_map: colMap,
      mode: "snapshot",
      write_mode: writeMode,
    };
    try {
      await create.mutateAsync(body);
      onClose();
    } catch {
      // toast surfaces the error
    }
  };

  return (
    <DialogSurface>
      <DialogBody>
        <DialogTitle>New migration plan</DialogTitle>
        <DialogContent>
          <div className={styles.formStack}>
            <Field label="Name" required>
              <Input
                value={name}
                onChange={(_, d) => setName(d.value)}
                placeholder="e.g. orders → analytics warehouse"
              />
            </Field>

            <Subtitle2>Source</Subtitle2>
            <Field label="Connection" required>
              <Dropdown
                value={sqlConns.find((c) => c.id === srcConn)?.name ?? ""}
                selectedOptions={srcConn ? [srcConn] : []}
                onOptionSelect={(_, d) => setSrcConn(d.optionValue ?? "")}
                placeholder="Pick a source connection"
              >
                {sqlConns.map((c) => (
                  <Option key={c.id} value={c.id} text={c.name}>
                    {c.name}
                  </Option>
                ))}
              </Dropdown>
            </Field>
            <div className={styles.twoCol}>
              <Field
                label={
                  <InfoLabel info="Optional schema/database the source table lives in. Leave blank if the connection points at a single schema already.">
                    Schema
                  </InfoLabel>
                }
              >
                <Input
                  value={srcSchema}
                  onChange={(_, d) => setSrcSchema(d.value)}
                  placeholder="public"
                />
              </Field>
              <Field label="Table" required>
                <Input
                  value={srcTable}
                  onChange={(_, d) => setSrcTable(d.value)}
                  placeholder="orders"
                />
              </Field>
            </div>

            <Subtitle2>Destination</Subtitle2>
            <Field label="Connection" required>
              <Dropdown
                value={sqlConns.find((c) => c.id === dstConn)?.name ?? ""}
                selectedOptions={dstConn ? [dstConn] : []}
                onOptionSelect={(_, d) => setDstConn(d.optionValue ?? "")}
                placeholder="Pick a destination connection"
              >
                {sqlConns.map((c) => (
                  <Option key={c.id} value={c.id} text={c.name}>
                    {c.name}
                  </Option>
                ))}
              </Dropdown>
            </Field>
            <div className={styles.twoCol}>
              <Field label="Schema">
                <Input
                  value={dstSchema}
                  onChange={(_, d) => setDstSchema(d.value)}
                  placeholder="analytics"
                />
              </Field>
              <Field label="Table" required>
                <Input
                  value={dstTable}
                  onChange={(_, d) => setDstTable(d.value)}
                  placeholder="orders"
                />
              </Field>
            </div>

            <Subtitle2>Columns + write mode</Subtitle2>
            <Field
              label={
                <InfoLabel info="Comma-separated column names. v0 uses identity mapping — the same column name on both sides. Full src→dest mapping is in the next iteration.">
                  Columns
                </InfoLabel>
              }
              required
            >
              <Input
                value={columnsRaw}
                onChange={(_, d) => setColumnsRaw(d.value)}
                placeholder="id, email, created_at"
              />
            </Field>
            <Field
              label={
                <InfoLabel info="truncate_and_replace clears the destination table before writing; append adds rows without clearing.">
                  Write mode
                </InfoLabel>
              }
            >
              <Dropdown
                value={writeMode}
                selectedOptions={[writeMode]}
                onOptionSelect={(_, d) => setWriteMode(d.optionValue as "truncate_and_replace" | "append")}
              >
                <Option value="truncate_and_replace">truncate_and_replace</Option>
                <Option value="append">append</Option>
              </Dropdown>
            </Field>

            <Caption1 className={styles.meta}>
              Snapshot mode is the only supported mode today. Incremental + CDC ship in a follow-up.
            </Caption1>
          </div>
        </DialogContent>
        <DialogActions>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            appearance="primary"
            disabled={!valid || create.isPending}
            onClick={submit}
          >
            {create.isPending ? <Spinner size="extra-tiny" /> : "Create plan"}
          </Button>
        </DialogActions>
      </DialogBody>
    </DialogSurface>
  );
}

// helpers --------------------------------------------------------------

function qual(schema: string, table: string): string {
  return schema ? `${schema}.${table}` : table;
}

function msBetween(a: string, b: string): number {
  return new Date(b).getTime() - new Date(a).getTime();
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}
