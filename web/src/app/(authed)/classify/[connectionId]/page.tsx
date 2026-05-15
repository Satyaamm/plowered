"use client";

import Link from "next/link";
import { Fragment, use, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Badge,
  Body1,
  Button,
  Card,
  Caption1,
  Checkbox,
  Combobox,
  Divider,
  Field,
  MessageBar,
  MessageBarBody,
  MessageBarTitle,
  Option,
  OptionGroup,
  Slider,
  Spinner,
  Subtitle1,
  Subtitle2,
  Text,
  Title3,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  ArrowLeft20Regular,
  CheckmarkCircle20Filled,
  Eye20Regular,
  ShieldCheckmark20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { ErrorBanner, LoadingState } from "@/components/states";
import { InfoLabel } from "@/components/info-label";
import {
  ClassifyProposal,
  ClassifyProposalColumn,
  ConnectionScope,
  useClassifyApply,
  useClassifyPreview,
  useConnections,
  useConnectionScope,
} from "@/lib/hooks";

type Step = "configure" | "review" | "applying" | "done";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  stepRail: {
    display: "flex",
    alignItems: "center",
    gap: "0",
    padding: "14px 18px",
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
  },
  stepChip: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    fontSize: "13px",
    color: tokens.colorNeutralForeground3,
  },
  stepNumber: {
    width: "24px",
    height: "24px",
    borderRadius: "999px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    fontSize: "12px",
    fontWeight: 600,
    backgroundColor: tokens.colorNeutralBackground3,
    color: tokens.colorNeutralForeground3,
  },
  stepNumberActive: {
    backgroundColor: tokens.colorBrandBackground,
    color: tokens.colorNeutralForegroundInverted,
  },
  stepNumberDone: {
    backgroundColor: tokens.colorPaletteGreenBackground3,
    color: tokens.colorNeutralForegroundInverted,
  },
  stepLabelActive: { color: tokens.colorNeutralForeground1, fontWeight: 600 },
  stepConnector: {
    flex: 1,
    height: "2px",
    backgroundColor: tokens.colorNeutralStroke2,
    margin: "0 12px",
  },
  stepConnectorDone: { backgroundColor: tokens.colorPaletteGreenBackground3 },
  card: {
    padding: "20px",
    display: "flex",
    flexDirection: "column",
    gap: "16px",
  },
  cardHeader: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
  },
  cardHeaderRow: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    flexWrap: "wrap",
  },
  configRow: {
    display: "grid",
    gridTemplateColumns: "1fr 1fr",
    gap: "16px",
  },
  sliderRow: {
    display: "grid",
    gridTemplateColumns: "1fr auto",
    alignItems: "center",
    gap: "16px",
  },
  thresholdValue: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "20px",
    fontWeight: 600,
    minWidth: "56px",
    textAlign: "right",
    color: tokens.colorBrandForeground1,
  },
  tableCard: {
    display: "flex",
    flexDirection: "column",
    padding: "0",
    overflow: "hidden",
  },
  tableHead: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "12px 16px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    backgroundColor: tokens.colorNeutralBackground2,
  },
  colRow: {
    display: "grid",
    gridTemplateColumns: "32px 1.4fr 1fr 1.2fr 100px",
    gap: "12px",
    alignItems: "center",
    padding: "10px 16px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke3}`,
  },
  colRowLast: { borderBottom: "none" },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorNeutralForeground2,
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  tagRow: { display: "flex", flexWrap: "wrap", gap: "4px" },
  rightCol: { textAlign: "right" },
  actions: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    paddingTop: "8px",
  },
  summaryGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(4, 1fr)",
    gap: "12px",
  },
  summaryCell: {
    padding: "12px 14px",
    backgroundColor: tokens.colorNeutralBackground2,
    borderRadius: "6px",
    display: "flex",
    flexDirection: "column",
    gap: "4px",
  },
  summaryNumber: { fontSize: "22px", fontWeight: 600 },
});

// Tags the user can choose to keep vs drop. We render each proposed tag
// as a togglable Badge; the user's selected set is the source of truth
// for what gets POSTed in classify:apply.
type DecisionMap = Record<string, Set<string>>;

export default function ClassifyWizardPage({
  params,
}: {
  params: Promise<{ connectionId: string }>;
}) {
  const styles = useStyles();
  const router = useRouter();
  const { connectionId } = use(params);

  const connections = useConnections();
  const connection = connections.data?.find((c) => c.id === connectionId);
  const scope = useConnectionScope(connectionId);

  const [step, setStep] = useState<Step>("configure");
  const [schemas, setSchemas] = useState<string[]>([]);
  const [tables, setTables] = useState<string[]>([]);
  const [maskSamples] = useState(true);
  const [autoThreshold, setAutoThreshold] = useState(0.9);

  const preview = useClassifyPreview(connectionId);
  const apply = useClassifyApply(connectionId);

  const [decisions, setDecisions] = useState<DecisionMap>({});

  const startPreview = async () => {
    const body = {
      schemas: schemas.length > 0 ? schemas : undefined,
      tables: tables.length > 0 ? tables : undefined,
    };
    const proposal = await preview.mutateAsync(body);
    setDecisions(seedDecisions(proposal, autoThreshold));
    setStep("review");
  };

  const totals = useMemo(() => {
    if (!preview.data) return null;
    let tables = preview.data.tables.length;
    let columns = 0;
    let proposed = 0;
    let accepted = 0;
    for (const t of preview.data.tables) {
      for (const c of t.columns) {
        columns++;
        if (c.proposed_tags.length > 0) proposed++;
        const sel = decisions[c.asset_id];
        if (sel && sel.size > 0) accepted++;
      }
    }
    return { tables, columns, proposed, accepted };
  }, [preview.data, decisions]);

  const onApply = async () => {
    if (!preview.data) return;
    setStep("applying");
    const payload = Object.entries(decisions)
      .filter(([, tags]) => tags.size > 0)
      .map(([column_asset_id, tags]) => ({
        column_asset_id,
        tags: Array.from(tags),
      }));
    try {
      await apply.mutateAsync(payload);
      setStep("done");
    } catch {
      setStep("review");
    }
  };

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[
          { label: "Home", href: "/" },
          { label: "Connections", href: "/connections" },
          { label: connection?.name ?? "Classify" },
        ]}
        title="Classify connection"
        subtitle="Sample rows from this warehouse, detect sensitive patterns, then approve what gets tagged."
        actions={
          <Button
            icon={<ArrowLeft20Regular />}
            onClick={() => router.push("/connections")}
          >
            Back
          </Button>
        }
      />

      <StepRail step={step} />

      {step === "configure" && (
        <ConfigureStep
          connectionType={connection?.type}
          scope={scope.data}
          scopeLoading={scope.isLoading}
          scopeError={scope.error as Error | null}
          schemas={schemas}
          setSchemas={setSchemas}
          tables={tables}
          setTables={setTables}
          onStart={startPreview}
          loading={preview.isPending}
          error={preview.error as Error | null}
        />
      )}

      {step === "review" && preview.data && totals && (
        <ReviewStep
          proposal={preview.data}
          decisions={decisions}
          setDecisions={setDecisions}
          totals={totals}
          maskSamples={maskSamples}
          autoThreshold={autoThreshold}
          setAutoThreshold={(v) => {
            setAutoThreshold(v);
            if (preview.data) setDecisions(seedDecisions(preview.data, v));
          }}
          onBack={() => setStep("configure")}
          onApply={onApply}
          applyDisabled={totals.accepted === 0 || apply.isPending}
        />
      )}

      {step === "applying" && <LoadingState />}

      {step === "done" && apply.data && (
        <DoneStep
          applied={apply.data.applied}
          columnsUpdated={apply.data.columns_updated}
          onReturn={() => router.push("/connections")}
        />
      )}
    </div>
  );
}

// ----- step rail --------------------------------------------------------

function StepRail({ step }: { step: Step }) {
  const styles = useStyles();
  const order: Step[] = ["configure", "review", "applying"];
  const currentIdx = step === "done" ? 3 : order.indexOf(step);

  const stage = (idx: number) => {
    if (idx < currentIdx) return "done" as const;
    if (idx === currentIdx) return "active" as const;
    return "todo" as const;
  };

  const STEPS: { label: string; step: Step }[] = [
    { label: "Configure", step: "configure" },
    { label: "Review", step: "review" },
    { label: "Apply", step: "applying" },
  ];

  return (
    <div className={styles.stepRail}>
      {STEPS.map((s, i) => {
        const st = stage(i);
        const numCls =
          st === "active"
            ? `${styles.stepNumber} ${styles.stepNumberActive}`
            : st === "done"
              ? `${styles.stepNumber} ${styles.stepNumberDone}`
              : styles.stepNumber;
        const labelCls =
          st === "active"
            ? `${styles.stepChip} ${styles.stepLabelActive}`
            : styles.stepChip;
        return (
          <Fragment key={s.step}>
            <span className={labelCls}>
              <span className={numCls}>
                {st === "done" ? (
                  <CheckmarkCircle20Filled style={{ width: 16, height: 16 }} />
                ) : (
                  i + 1
                )}
              </span>
              {s.label}
            </span>
            {i < STEPS.length - 1 && (
              <span
                className={
                  st === "done"
                    ? `${styles.stepConnector} ${styles.stepConnectorDone}`
                    : styles.stepConnector
                }
              />
            )}
          </Fragment>
        );
      })}
    </div>
  );
}

// ----- step 1: configure ------------------------------------------------

function ConfigureStep({
  connectionType,
  scope,
  scopeLoading,
  scopeError,
  schemas,
  setSchemas,
  tables,
  setTables,
  onStart,
  loading,
  error,
}: {
  connectionType?: string;
  scope?: ConnectionScope;
  scopeLoading: boolean;
  scopeError: Error | null;
  schemas: string[];
  setSchemas: (v: string[]) => void;
  tables: string[];
  setTables: (v: string[]) => void;
  onStart: () => void;
  loading: boolean;
  error: Error | null;
}) {
  const styles = useStyles();

  // Tables shown in the second dropdown filter by the picked schemas.
  // When no schema is picked, all tables are shown — empty schema = all.
  // We group options by schema so the operator can see what belongs
  // where; the option `value` is just the bare table name (matches
  // backend's case-insensitive table-name filter).
  const tablesByVisibleSchema = useMemo(() => {
    if (!scope) return new Map<string, string[]>();
    const want =
      schemas.length === 0 ? null : new Set(schemas.map((s) => s.toLowerCase()));
    const out = new Map<string, string[]>();
    for (const t of scope.tables) {
      if (want && !want.has(t.schema.toLowerCase())) continue;
      const list = out.get(t.schema) ?? [];
      list.push(t.name);
      out.set(t.schema, list);
    }
    return out;
  }, [scope, schemas]);

  // Drop tables that are no longer visible after a schema change. Keeps
  // the state coherent — selecting `analytics`, then unchecking it,
  // would otherwise leave `analytics.users` selected with no way to see
  // or remove it.
  const visibleTableNames = useMemo(() => {
    const out = new Set<string>();
    for (const list of tablesByVisibleSchema.values()) {
      for (const n of list) out.add(n.toLowerCase());
    }
    return out;
  }, [tablesByVisibleSchema]);

  const filteredTables = tables.filter((t) =>
    visibleTableNames.has(t.toLowerCase()),
  );

  const totalTables = scope?.tables.length ?? 0;
  const matchedCount = useMemo(() => {
    if (!scope) return 0;
    const want =
      schemas.length === 0 ? null : new Set(schemas.map((s) => s.toLowerCase()));
    const wantT =
      tables.length === 0 ? null : new Set(tables.map((t) => t.toLowerCase()));
    let n = 0;
    for (const t of scope.tables) {
      if (want && !want.has(t.schema.toLowerCase())) continue;
      if (wantT && !wantT.has(t.name.toLowerCase())) continue;
      n++;
    }
    return n;
  }, [scope, schemas, tables]);

  return (
    <>
      <Card className={styles.card}>
        <div className={styles.cardHeader}>
          <div className={styles.cardHeaderRow}>
            <Title3>What classify does</Title3>
            {connectionType && (
              <Badge appearance="tint" color="brand">
                {connectionType}
              </Badge>
            )}
            <span style={{ flex: 1 }} />
            <Caption1 className={styles.meta}>
              ~200 rows sampled per table
            </Caption1>
          </div>
          <Body1>
            Plowered samples a small number of rows from every table in this
            connection&apos;s catalog and runs nine local regex detectors
            (email, phone, SSN, credit card, IP, URL, UUID, IBAN, secrets)
            against each column. No values leave your warehouse — the
            matching happens in-process. The next step lets you review every
            proposed tag before tuning a confidence threshold and applying.
          </Body1>
        </div>
      </Card>

      <Card className={styles.card}>
        <div className={styles.cardHeader}>
          <div className={styles.cardHeaderRow}>
            <Subtitle1>Scope</Subtitle1>
            <span style={{ flex: 1 }} />
            <Caption1 className={styles.meta}>
              {scopeLoading
                ? "loading catalog…"
                : `${matchedCount} of ${totalTables} tables will be scanned`}
            </Caption1>
          </div>
          <Caption1 className={styles.meta}>
            Restrict scanning by schema or specific table. Leave both empty
            to scan everything in the catalog.
          </Caption1>
        </div>
        <Divider />
        {scopeError && <ErrorBanner error={scopeError} />}
        <div className={styles.configRow}>
          <Field
            label={
              <InfoLabel info="Pick one or more schemas the catalog has indexed for this connection. Empty = every schema. Selecting a schema narrows the Tables dropdown below.">
                Schemas
              </InfoLabel>
            }
          >
            <Combobox
              multiselect
              freeform={false}
              disabled={scopeLoading || !scope}
              selectedOptions={schemas}
              value={
                schemas.length === 0
                  ? ""
                  : schemas.length <= 2
                    ? schemas.join(", ")
                    : `${schemas.length} schemas selected`
              }
              placeholder={
                scopeLoading
                  ? "Loading…"
                  : scope && scope.schemas.length === 0
                    ? "No schemas in this connection yet"
                    : "All schemas"
              }
              onOptionSelect={(_, d) => {
                setSchemas(d.selectedOptions);
                // Drop now-hidden tables from the table selection.
                if (d.selectedOptions.length > 0 && scope) {
                  const allowed = new Set(
                    scope.tables
                      .filter((t) =>
                        d.selectedOptions
                          .map((s) => s.toLowerCase())
                          .includes(t.schema.toLowerCase()),
                      )
                      .map((t) => t.name.toLowerCase()),
                  );
                  setTables(tables.filter((t) => allowed.has(t.toLowerCase())));
                }
              }}
            >
              {scope?.schemas.map((s) => (
                <Option key={s} value={s}>
                  {s}
                </Option>
              ))}
            </Combobox>
          </Field>
          <Field
            label={
              <InfoLabel info="Pick specific tables to scan. Empty = every table inside the selected schemas (or every table in the connection if no schema is selected). Useful when piloting against one or two tables before a full sweep.">
                Tables
              </InfoLabel>
            }
          >
            <Combobox
              multiselect
              freeform={false}
              disabled={scopeLoading || !scope || visibleTableNames.size === 0}
              selectedOptions={filteredTables}
              value={
                filteredTables.length === 0
                  ? ""
                  : filteredTables.length <= 2
                    ? filteredTables.join(", ")
                    : `${filteredTables.length} tables selected`
              }
              placeholder={
                scopeLoading
                  ? "Loading…"
                  : visibleTableNames.size === 0
                    ? "No tables in scope"
                    : schemas.length === 0
                      ? "All tables (every schema)"
                      : "All tables in selected schemas"
              }
              onOptionSelect={(_, d) => setTables(d.selectedOptions)}
            >
              {Array.from(tablesByVisibleSchema.entries()).map(
                ([schemaName, tableList]) => (
                  <OptionGroup key={schemaName} label={schemaName}>
                    {tableList.map((name) => (
                      <Option key={`${schemaName}.${name}`} value={name}>
                        {name}
                      </Option>
                    ))}
                  </OptionGroup>
                ),
              )}
            </Combobox>
          </Field>
        </div>
      </Card>

      {error && <ErrorBanner error={error} />}

      <div className={styles.actions}>
        <Caption1 className={styles.meta}>
          Sampling is read-only — no writes happen until you confirm on Step 3.
        </Caption1>
        <Button
          appearance="primary"
          icon={loading ? <Spinner size="extra-tiny" /> : <Eye20Regular />}
          onClick={onStart}
          disabled={loading || scopeLoading || matchedCount === 0}
        >
          {loading
            ? "Sampling…"
            : `Preview ${matchedCount} table${matchedCount === 1 ? "" : "s"}`}
        </Button>
      </div>
    </>
  );
}

// ----- step 2: review ---------------------------------------------------

function ReviewStep({
  proposal,
  decisions,
  setDecisions,
  totals,
  maskSamples,
  autoThreshold,
  setAutoThreshold,
  onBack,
  onApply,
  applyDisabled,
}: {
  proposal: ClassifyProposal;
  decisions: DecisionMap;
  setDecisions: (next: DecisionMap) => void;
  totals: { tables: number; columns: number; proposed: number; accepted: number };
  maskSamples: boolean;
  autoThreshold: number;
  setAutoThreshold: (v: number) => void;
  onBack: () => void;
  onApply: () => void;
  applyDisabled: boolean;
}) {
  const styles = useStyles();

  const toggleTag = (colId: string, tag: string) => {
    const next = { ...decisions };
    const set = new Set(next[colId] ?? []);
    if (set.has(tag)) set.delete(tag);
    else set.add(tag);
    next[colId] = set;
    setDecisions(next);
  };

  return (
    <>
      <div className={styles.summaryGrid}>
        <SummaryCell label="Tables scanned" value={totals.tables} />
        <SummaryCell label="Columns analysed" value={totals.columns} />
        <SummaryCell label="Tags proposed" value={totals.proposed} />
        <SummaryCell label="You accepted" value={totals.accepted} accent />
      </div>

      <Card className={styles.card}>
        <div className={styles.cardHeader}>
          <div className={styles.cardHeaderRow}>
            <Subtitle1>Confidence threshold</Subtitle1>
            <span style={{ flex: 1 }} />
            <Caption1 className={styles.meta}>
              Pre-accepts tags whose strongest detector matches at least this fraction of sampled rows
            </Caption1>
          </div>
          <div className={styles.sliderRow}>
            <Slider
              min={0.5}
              max={1}
              step={0.05}
              value={autoThreshold}
              onChange={(_, d) => setAutoThreshold(d.value)}
            />
            <span className={styles.thresholdValue}>
              {(autoThreshold * 100).toFixed(0)}%
            </span>
          </div>
          <Caption1 className={styles.meta}>
            Drag left to also auto-accept borderline matches, right to only
            keep the cleanest signals. Individual tags below stay toggleable.
          </Caption1>
        </div>
      </Card>

      {proposal.skipped.length > 0 && (
        <MessageBar intent="warning">
          <MessageBarBody>
            <MessageBarTitle>
              {proposal.skipped.length} table{proposal.skipped.length === 1 ? "" : "s"} skipped
            </MessageBarTitle>
            {proposal.skipped
              .slice(0, 3)
              .map((s) => `${s.table}: ${s.reason}`)
              .join(" • ")}
            {proposal.skipped.length > 3 && ` (+${proposal.skipped.length - 3} more)`}
          </MessageBarBody>
        </MessageBar>
      )}

      {proposal.tables.map((t) => (
        <Card key={t.asset_id} className={styles.tableCard}>
          <div className={styles.tableHead}>
            <Subtitle2>{t.name}</Subtitle2>
            <Caption1 className={styles.meta}>
              {t.schema ? `${t.schema}.${t.name}` : t.name}
            </Caption1>
            <span style={{ flex: 1 }} />
            <Badge appearance="tint" color="subtle">
              {t.columns.length} columns
            </Badge>
          </div>
          {t.columns.map((c, i) => (
            <ColumnRow
              key={c.asset_id}
              col={c}
              isLast={i === t.columns.length - 1}
              selected={decisions[c.asset_id] ?? new Set()}
              onToggle={(tag) => toggleTag(c.asset_id, tag)}
              maskSamples={maskSamples}
            />
          ))}
        </Card>
      ))}

      <div className={styles.actions}>
        <Button onClick={onBack}>Back</Button>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <Caption1 className={styles.meta}>
            {totals.accepted} of {totals.columns} columns will be tagged.
          </Caption1>
          <Button
            appearance="primary"
            icon={<ShieldCheckmark20Regular />}
            disabled={applyDisabled}
            onClick={onApply}
          >
            Apply {totals.accepted} tag{totals.accepted === 1 ? "" : "s"}
          </Button>
        </div>
      </div>
    </>
  );
}

function ColumnRow({
  col,
  isLast,
  selected,
  onToggle,
  maskSamples,
}: {
  col: ClassifyProposalColumn;
  isLast: boolean;
  selected: Set<string>;
  onToggle: (tag: string) => void;
  maskSamples: boolean;
}) {
  const styles = useStyles();
  const proposed = col.proposed_tags;
  const allAccepted = proposed.length > 0 && proposed.every((t) => selected.has(t));
  const noneAccepted = !proposed.some((t) => selected.has(t));
  const topHits = Object.entries(col.hits ?? {})
    .filter(([, n]) => n > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 3);

  return (
    <div className={`${styles.colRow} ${isLast ? styles.colRowLast : ""}`}>
      <Checkbox
        checked={proposed.length === 0 ? false : allAccepted ? true : noneAccepted ? false : "mixed"}
        disabled={proposed.length === 0}
        onChange={(_, d) => {
          // Bulk toggle every proposed tag on/off.
          for (const t of proposed) {
            const has = selected.has(t);
            if (d.checked && !has) onToggle(t);
            else if (!d.checked && has) onToggle(t);
          }
        }}
        aria-label={`Accept all proposed tags for ${col.name}`}
      />
      <div>
        <Text weight="semibold">{col.name}</Text>
        <Caption1 className={styles.meta}>
          {col.sampled > 0 ? `${col.sampled} rows sampled` : "no rows sampled"}
          {maskSamples ? " · values masked" : ""}
        </Caption1>
      </div>
      <div className={styles.tagRow}>
        {proposed.length === 0 ? (
          <Caption1 className={styles.meta}>no detections</Caption1>
        ) : (
          proposed.map((tag) => {
            const on = selected.has(tag);
            return (
              <Tooltip
                key={tag}
                content={`Click to ${on ? "drop" : "accept"} this tag`}
                relationship="label"
              >
                <button
                  type="button"
                  onClick={() => onToggle(tag)}
                  style={{
                    border: "none",
                    cursor: "pointer",
                    padding: 0,
                    background: "transparent",
                  }}
                  aria-pressed={on}
                >
                  <Badge
                    appearance={on ? "filled" : "outline"}
                    color={tagColor(tag)}
                  >
                    {tag.replace(/^class:/, "")}
                  </Badge>
                </button>
              </Tooltip>
            );
          })
        )}
      </div>
      <div className={styles.tagRow}>
        {topHits.length === 0 ? (
          <Caption1 className={styles.meta}>—</Caption1>
        ) : (
          topHits.map(([kind, n]) => (
            <Caption1 key={kind} className={styles.mono}>
              {kind.replace(/^class:/, "")}: {n}/{col.sampled}
            </Caption1>
          ))
        )}
      </div>
      <div className={`${styles.rightCol} ${styles.mono}`}>
        {proposed.length > 0 && (
          <Link
            href={`/asset/${encodeURIComponent(col.asset_id)}`}
            style={{ color: "inherit" }}
          >
            view
          </Link>
        )}
      </div>
    </div>
  );
}

function tagColor(
  tag: string,
): "brand" | "danger" | "informative" | "warning" | "success" | "subtle" {
  if (
    tag.startsWith("class:secret") ||
    tag.startsWith("class:phi") ||
    tag.startsWith("class:pci")
  )
    return "danger";
  if (tag.startsWith("class:pii")) return "warning";
  return "informative";
}

function SummaryCell({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent?: boolean;
}) {
  const styles = useStyles();
  return (
    <div className={styles.summaryCell}>
      <Caption1 className={styles.meta}>{label}</Caption1>
      <span
        className={styles.summaryNumber}
        style={accent ? { color: tokens.colorBrandForeground1 } : undefined}
      >
        {value}
      </span>
    </div>
  );
}

// ----- step 3: done -----------------------------------------------------

function DoneStep({
  applied,
  columnsUpdated,
  onReturn,
}: {
  applied: number;
  columnsUpdated: number;
  onReturn: () => void;
}) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <CheckmarkCircle20Filled style={{ color: tokens.colorPaletteGreenForeground2 }} />
        <Title3>Classification applied</Title3>
      </div>
      <Text>
        {applied} tag{applied === 1 ? "" : "s"} were written across{" "}
        {columnsUpdated} column{columnsUpdated === 1 ? "" : "s"}. Detected
        classifications are visible on every asset&apos;s detail page and in
        the catalog tag column.
      </Text>
      <div>
        <Button appearance="primary" onClick={onReturn}>
          Back to connections
        </Button>
      </div>
    </Card>
  );
}

// ----- helpers ----------------------------------------------------------

// seedDecisions pre-checks every proposed tag whose strongest detector hit
// is at or above the user's auto-accept threshold. Below threshold the
// tag is left for the user to pick manually so they can't accidentally
// rubber-stamp noisy detections.
function seedDecisions(
  proposal: ClassifyProposal,
  threshold: number,
): DecisionMap {
  const out: DecisionMap = {};
  for (const t of proposal.tables) {
    for (const c of t.columns) {
      if (c.proposed_tags.length === 0 || c.sampled === 0) continue;
      const strongest = Math.max(
        0,
        ...Object.values(c.hits ?? {}).map((n) => n / c.sampled),
      );
      if (strongest >= threshold) {
        out[c.asset_id] = new Set(c.proposed_tags);
      }
    }
  }
  return out;
}
