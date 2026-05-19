"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Caption1,
  Card,
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
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { call } from "@/lib/hooks/_fetch";

// ProfileColumn / ProfileReport mirror the JSON shape of profile.Report
// on the backend. Defined locally rather than importing from profile.tsx
// so the two tabs don't tangle their types.
interface ProfileColumn {
  name: string;
  data_type: string;
  rows_sampled: number;
  null_count: number;
  distinct_count: number;
  min?: string;
  max?: string;
  mean?: number;
  top_values?: { value: string; count: number }[];
}

interface ProfileReport {
  table_asset_id: string;
  schema: string;
  table: string;
  generated_at: string;
  rows_scanned: number;
  columns: ProfileColumn[];
}

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "16px" },
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
  },
  panelHead: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "10px",
    flexWrap: "wrap",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  twoCol: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(380px, 1fr))",
    gap: "12px",
  },
  statsRow: {
    display: "grid",
    gridTemplateColumns: "repeat(4, 1fr)",
    gap: "8px",
    paddingTop: "4px",
  },
  stat: {
    padding: "8px 10px",
    backgroundColor: tokens.colorNeutralBackground2,
    borderRadius: "4px",
    display: "flex",
    flexDirection: "column",
    gap: "2px",
  },
  statLabel: {
    color: tokens.colorNeutralForeground3,
    fontSize: "11px",
    textTransform: "uppercase",
    letterSpacing: "0.04em",
  },
  statValue: {
    fontSize: "16px",
    fontWeight: 600,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
  },
});

export function ExploreTab({ assetId }: { assetId: string }) {
  const styles = useStyles();
  const profile = useQuery({
    queryKey: ["asset-profile", assetId],
    queryFn: () => call<ProfileReport>("GET", `/v1/assets/${assetId}/profile`),
    retry: false,
    staleTime: 60_000,
  });

  if (profile.isLoading) return <LoadingState />;
  if (profile.error) return <ErrorBanner error={profile.error as Error} />;
  if (!profile.data) {
    return (
      <EmptyState
        title="Profile not computed yet"
        body="Visit the Profile tab and click Refresh to compute statistics. Charts here read from the same cache."
      />
    );
  }

  return (
    <div className={styles.body}>
      <NullRatioPanel report={profile.data} />
      <CategoricalCharts report={profile.data} />
      <NumericStats report={profile.data} />
    </div>
  );
}

// ----- chart 1: null ratio across all columns ------------------------

function NullRatioPanel({ report }: { report: ProfileReport }) {
  const styles = useStyles();
  const data = useMemo(() => {
    return report.columns
      .map((c) => ({
        name: c.name,
        nullPct:
          c.rows_sampled > 0
            ? Number(((c.null_count / c.rows_sampled) * 100).toFixed(1))
            : 0,
      }))
      .sort((a, b) => b.nullPct - a.nullPct)
      .slice(0, 20); // top-20 most-null cols
  }, [report]);

  return (
    <Card className={styles.panel}>
      <div className={styles.panelHead}>
        <Subtitle1>Null ratio per column</Subtitle1>
        <Caption1 className={styles.meta}>
          Top 20 columns by missing data, sampled from {report.rows_scanned.toLocaleString()} rows
        </Caption1>
      </div>
      <ResponsiveContainer width="100%" height={Math.max(160, data.length * 22)}>
        <BarChart
          data={data}
          layout="vertical"
          margin={{ top: 4, right: 24, left: 8, bottom: 4 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke={tokens.colorNeutralStroke3} />
          <XAxis
            type="number"
            domain={[0, 100]}
            tickFormatter={(v) => `${v}%`}
            fontSize={11}
          />
          <YAxis
            type="category"
            dataKey="name"
            width={140}
            tick={{ fontSize: 11 }}
            interval={0}
          />
          <Tooltip
            formatter={(v: number) => [`${v}%`, "Nulls"]}
            cursor={{ fill: tokens.colorNeutralBackground2 }}
          />
          <Bar dataKey="nullPct" radius={[0, 3, 3, 0]}>
            {data.map((d, i) => (
              <Cell
                key={i}
                fill={
                  d.nullPct > 50
                    ? tokens.colorPaletteRedBackground2
                    : d.nullPct > 10
                      ? tokens.colorPaletteYellowBackground2
                      : tokens.colorPaletteGreenBackground2
                }
              />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </Card>
  );
}

// ----- chart 2: top-values per categorical column --------------------

function CategoricalCharts({ report }: { report: ProfileReport }) {
  const styles = useStyles();
  // A "categorical" column for chart purposes: has top_values and the
  // distinct count is reasonably small relative to row count (otherwise
  // top-5 of a column with 1M distinct values is noise).
  const cols = useMemo(() => {
    return report.columns.filter((c) => {
      if (!c.top_values || c.top_values.length === 0) return false;
      if (c.rows_sampled === 0) return false;
      if (c.distinct_count > c.rows_sampled * 0.5) return false;
      return true;
    });
  }, [report]);

  if (cols.length === 0) return null;

  return (
    <Card className={styles.panel}>
      <div className={styles.panelHead}>
        <Subtitle1>Top values</Subtitle1>
        <Caption1 className={styles.meta}>
          Categorical columns with low cardinality — each chart shows the {cols[0].top_values?.length ?? 5} most common values
        </Caption1>
      </div>
      <div className={styles.twoCol}>
        {cols.slice(0, 6).map((c) => (
          <TopValuesChart key={c.name} col={c} />
        ))}
      </div>
    </Card>
  );
}

function TopValuesChart({ col }: { col: ProfileColumn }) {
  const styles = useStyles();
  const data = (col.top_values ?? []).map((tv) => ({
    name: tv.value.length > 28 ? tv.value.slice(0, 27) + "…" : tv.value,
    count: tv.count,
  }));
  return (
    <div>
      <div className={styles.panelHead}>
        <Subtitle2>{col.name}</Subtitle2>
        <Badge appearance="tint" color="subtle">
          {col.distinct_count.toLocaleString()} distinct
        </Badge>
      </div>
      <ResponsiveContainer width="100%" height={Math.max(120, data.length * 28)}>
        <BarChart
          data={data}
          layout="vertical"
          margin={{ top: 4, right: 8, left: 8, bottom: 4 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke={tokens.colorNeutralStroke3} />
          <XAxis type="number" fontSize={11} />
          <YAxis
            type="category"
            dataKey="name"
            width={160}
            tick={{ fontSize: 11 }}
            interval={0}
          />
          <Tooltip
            formatter={(v: number) => [v.toLocaleString(), "rows"]}
            cursor={{ fill: tokens.colorNeutralBackground2 }}
          />
          <Bar dataKey="count" fill={tokens.colorBrandBackground} radius={[0, 3, 3, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

// ----- panel 3: numeric stats summary --------------------------------

function NumericStats({ report }: { report: ProfileReport }) {
  const styles = useStyles();
  const cols = report.columns.filter((c) => c.mean !== undefined);
  if (cols.length === 0) return null;
  return (
    <Card className={styles.panel}>
      <div className={styles.panelHead}>
        <Subtitle1>Numeric summary</Subtitle1>
        <Caption1 className={styles.meta}>min · max · mean per numeric column</Caption1>
      </div>
      {cols.map((c) => (
        <div key={c.name}>
          <Subtitle2>
            {c.name}{" "}
            <Caption1 className={styles.meta}>· {c.data_type}</Caption1>
          </Subtitle2>
          <div className={styles.statsRow}>
            <Stat label="min" value={c.min ?? "—"} />
            <Stat label="max" value={c.max ?? "—"} />
            <Stat label="mean" value={c.mean !== undefined ? c.mean.toFixed(2) : "—"} />
            <Stat
              label="distinct"
              value={c.distinct_count.toLocaleString()}
            />
          </div>
        </div>
      ))}
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  const styles = useStyles();
  return (
    <div className={styles.stat}>
      <Text className={styles.statLabel}>{label}</Text>
      <Text className={styles.statValue}>{value}</Text>
    </div>
  );
}
