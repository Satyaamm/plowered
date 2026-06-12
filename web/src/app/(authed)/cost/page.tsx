"use client";

import { useMemo, useState } from "react";
import {
  Badge,
  Caption1,
  Card,
  CardHeader,
  Dropdown,
  Option,
  Subtitle1,
  Subtitle2,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { PageHeader } from "@/components/page-header";
import { PageIntro } from "@/components/page-intro";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { useCostSummary, useRecentCost } from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  headlineRow: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
    gap: "12px",
  },
  card: { padding: "16px" },
  big: { fontSize: "28px", fontWeight: 600 },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  chartWrap: { width: "100%", height: "280px" },
  tableRow: {
    display: "grid",
    gridTemplateColumns: "180px 140px 120px 1fr 100px",
    gap: "8px",
    fontSize: "12px",
    padding: "4px 0",
    alignItems: "center",
  },
  tableHead: {
    fontWeight: 600,
    textTransform: "uppercase",
    fontSize: "11px",
    letterSpacing: "0.04em",
    color: tokens.colorNeutralForeground3,
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    marginBottom: "4px",
  },
});

const RANGE_OPTIONS = [
  { value: 7, label: "Last 7 days" },
  { value: 30, label: "Last 30 days" },
  { value: 90, label: "Last 90 days" },
];

// Stable palette so a given (kind, provider) stays the same colour
// across renders. Matches Plowered's Loamy accent + neutral tones.
const palette = [
  "#F38020", "#7A6A55", "#B89F77", "#5C7F4E", "#8B5E3C", "#3E4E5E",
];

export default function CostPage() {
  const styles = useStyles();
  const [range, setRange] = useState(30);
  const summary = useCostSummary(range);
  const recent = useRecentCost(50);

  const chart = useMemo(() => {
    if (!summary.data?.daily) return { rows: [], series: [] as string[] };
    // Pivot daily totals into (day -> {key: cost}) rows where key is
    // "kind|provider". Recharts wants one row per day with named series.
    const byDay = new Map<string, Record<string, any>>();
    const seriesSet = new Set<string>();
    for (const d of summary.data.daily) {
      const dayKey = d.day.slice(0, 10);
      const seriesKey = `${d.kind}/${d.provider}`;
      seriesSet.add(seriesKey);
      const row = byDay.get(dayKey) ?? { day: dayKey };
      row[seriesKey] = (row[seriesKey] ?? 0) + d.cost_usd;
      byDay.set(dayKey, row);
    }
    return {
      rows: Array.from(byDay.values()).sort((a, b) =>
        a.day.localeCompare(b.day),
      ),
      series: Array.from(seriesSet),
    };
  }, [summary.data]);

  return (
    <div className={styles.root}>
      <PageHeader
        title="Cost"
        subtitle="Per-tenant spend on AI completions + warehouse compute. Warehouse figures are wall-clock estimates; AI uses the published per-model token rates."
        crumbs={[{ label: "Home", href: "/" }, { label: "Cost" }]}
        actions={
          <>
            <PageIntro
              title="What does cost tracking show me?"
              body="Every billable call the platform makes — LLM tokens, warehouse seconds, S3 bytes — gets a cost row. This page rolls them up per feature so you can see where the spend is going before the invoice arrives."
              bullets={[
                "Avoid surprise bills: a runaway agent or a forgotten SELECT * shows up on this chart the same day, not at month-end.",
                "Set budgets with warn (e.g. 80%) and hard (100%) thresholds — alerts fire through the notify dispatcher with 24-hour dedupe.",
                "Numbers are estimates from a per-model price book; reconcile against your vendor billing exports for invoice-accuracy.",
              ]}
              cta="Get started: pick a time range. Then add a budget under Management → Cost budgets to start receiving threshold alerts."
            />
            <Dropdown
              value={RANGE_OPTIONS.find((r) => r.value === range)?.label ?? ""}
              selectedOptions={[String(range)]}
              onOptionSelect={(_, d) => setRange(Number(d.optionValue) || 30)}
            >
              {RANGE_OPTIONS.map((r) => (
                <Option key={r.value} value={String(r.value)} text={r.label}>
                  {r.label}
                </Option>
              ))}
            </Dropdown>
          </>
        }
      />

      {summary.isLoading && <LoadingState />}
      {summary.error && <ErrorBanner error={summary.error as Error} />}

      {summary.data && (
        <>
          <div className={styles.headlineRow}>
            <HeadlineCard label={`Total · last ${range}d`} value={summary.data.total_usd} />
            {Object.entries(summary.data.by_kind).map(([k, v]) => (
              <HeadlineCard key={k} label={prettyKind(k)} value={v} />
            ))}
          </div>

          <Card className={styles.card}>
            <CardHeader header={<Subtitle2>Daily spend by kind + provider</Subtitle2>} />
            {chart.rows.length === 0 ? (
              <EmptyState
                title="No cost recorded yet"
                body="Run an AI suggestion, a text-to-SQL query, or a migration. Cost rows appear here once the platform records them."
              />
            ) : (
              <div className={styles.chartWrap}>
                <ResponsiveContainer>
                  <BarChart data={chart.rows} stackOffset="sign">
                    <CartesianGrid stroke="#E5E7EB" />
                    <XAxis dataKey="day" stroke="#7A6A55" />
                    <YAxis stroke="#7A6A55" tickFormatter={(v) => `$${v}`} />
                    <Tooltip
                      formatter={(v: number) => [`$${v.toFixed(4)}`, ""]}
                    />
                    <Legend />
                    {chart.series.map((s, i) => (
                      <Bar
                        key={s}
                        dataKey={s}
                        stackId="usd"
                        fill={palette[i % palette.length]}
                        isAnimationActive={false}
                      />
                    ))}
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </Card>

          <Card className={styles.card}>
            <CardHeader header={<Subtitle2>Recent records</Subtitle2>} />
            {recent.data && recent.data.length > 0 ? (
              <div>
                <div className={`${styles.tableRow} ${styles.tableHead}`}>
                  <span>When</span>
                  <span>Kind</span>
                  <span>Provider</span>
                  <span>Attributes</span>
                  <span style={{ textAlign: "right" }}>USD</span>
                </div>
                {recent.data.map((r) => (
                  <div key={r.id} className={styles.tableRow}>
                    <span>{new Date(r.ts).toLocaleString()}</span>
                    <span>
                      <Badge appearance="tint">{prettyKind(r.kind)}</Badge>
                    </span>
                    <span>{r.provider}</span>
                    <span className={styles.meta}>
                      {summariseAttributes(r.attributes)}
                    </span>
                    <span style={{ textAlign: "right", fontVariantNumeric: "tabular-nums" }}>
                      ${r.cost_usd.toFixed(6)}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <Text className={styles.meta}>No recent activity.</Text>
            )}
          </Card>
        </>
      )}
    </div>
  );
}

function HeadlineCard({ label, value }: { label: string; value: number }) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <Caption1 className={styles.meta}>{label}</Caption1>
      <Subtitle1 className={styles.big}>${value.toFixed(2)}</Subtitle1>
    </Card>
  );
}

function prettyKind(k: string): string {
  if (k === "ai_completion") return "AI completion";
  if (k === "warehouse_query") return "Warehouse query";
  return k;
}

function summariseAttributes(attrs?: Record<string, unknown>): string {
  if (!attrs) return "";
  const interesting = [
    "feature",
    "model",
    "input_tokens",
    "output_tokens",
    "elapsed_ms",
    "row_count",
    "plan_name",
  ];
  return interesting
    .filter((k) => attrs[k] !== undefined && attrs[k] !== null && attrs[k] !== "")
    .map((k) => `${k}=${attrs[k]}`)
    .join(" · ");
}
