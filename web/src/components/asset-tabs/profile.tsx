"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Caption1,
  Card,
  DataGrid,
  DataGridBody,
  DataGridCell,
  DataGridHeader,
  DataGridHeaderCell,
  DataGridRow,
  ProgressBar,
  TableColumnDefinition,
  Text,
  Tooltip,
  createTableColumn,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { ArrowSync20Regular } from "@fluentui/react-icons";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { call } from "@/lib/hooks/_fetch";

// ProfileColumn mirrors profile.Column on the backend. Pointer-y fields
// are optional on the wire (omitempty) so we treat them as | undefined.
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
  body: { display: "flex", flexDirection: "column", gap: "12px" },
  toolbar: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "10px 14px",
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  grid: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
  },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorNeutralForeground2,
  },
  nullBarWrap: { display: "flex", alignItems: "center", gap: "6px", minWidth: "160px" },
  topVals: { display: "flex", flexWrap: "wrap", gap: "4px" },
});

export function ProfileTab({ assetId }: { assetId: string }) {
  const styles = useStyles();
  const qc = useQueryClient();

  const profile = useQuery({
    queryKey: ["asset-profile", assetId],
    queryFn: () =>
      call<ProfileReport>("GET", `/v1/assets/${assetId}/profile`),
    retry: false,
    staleTime: 60_000,
  });

  const refresh = useMutation({
    mutationFn: () =>
      call<ProfileReport>("POST", `/v1/assets/${assetId}/profile:refresh`),
    onSuccess: (r) => qc.setQueryData(["asset-profile", assetId], r),
    meta: { successMessage: "Profile refreshed" },
  });

  if (profile.isLoading) return <LoadingState />;
  if (profile.error) {
    return <ErrorBanner error={profile.error as Error} />;
  }
  if (!profile.data) {
    return (
      <EmptyState
        title="No profile yet"
        body="Click Refresh to sample this table and compute per-column statistics."
        action={
          <Button
            appearance="primary"
            icon={<ArrowSync20Regular />}
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending}
          >
            Run profile
          </Button>
        }
      />
    );
  }
  return (
    <div className={styles.body}>
      <Card className={styles.toolbar}>
        <Text weight="semibold">{profile.data.columns.length} columns</Text>
        <Caption1 className={styles.meta}>·</Caption1>
        <Caption1 className={styles.meta}>
          {profile.data.rows_scanned.toLocaleString()} rows sampled
        </Caption1>
        <Caption1 className={styles.meta}>·</Caption1>
        <Caption1 className={styles.meta}>
          last refreshed {new Date(profile.data.generated_at).toLocaleString()}
        </Caption1>
        <span style={{ flex: 1 }} />
        <Button
          icon={<ArrowSync20Regular />}
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
          size="small"
        >
          {refresh.isPending ? "Refreshing…" : "Refresh"}
        </Button>
      </Card>

      <div className={styles.grid}>
        <ProfileGrid columns={profile.data.columns} />
      </div>
    </div>
  );
}

function ProfileGrid({ columns }: { columns: ProfileColumn[] }) {
  const styles = useStyles();

  // null-percentage rendering is the single most useful glanceable
  // signal in column profiling — green = clean, red = mostly null.
  // Reusable as a render cell.
  const nullPctCell = (c: ProfileColumn) => {
    if (c.rows_sampled === 0)
      return <Caption1 className={styles.meta}>—</Caption1>;
    const pct = (c.null_count / c.rows_sampled) * 100;
    const color =
      pct > 50 ? "error" : pct > 10 ? "warning" : "success";
    return (
      <div className={styles.nullBarWrap}>
        <ProgressBar
          value={c.null_count / c.rows_sampled}
          color={color}
          thickness="medium"
        />
        <span className={styles.mono}>{pct.toFixed(1)}%</span>
      </div>
    );
  };

  const cols: TableColumnDefinition<ProfileColumn>[] = [
    createTableColumn<ProfileColumn>({
      columnId: "name",
      renderHeaderCell: () => "Column",
      renderCell: (c) => (
        <div>
          <Text weight="semibold">{c.name}</Text>
          <Caption1 className={styles.meta}> · {c.data_type}</Caption1>
        </div>
      ),
    }),
    createTableColumn<ProfileColumn>({
      columnId: "nulls",
      renderHeaderCell: () => "Nulls",
      renderCell: nullPctCell,
    }),
    createTableColumn<ProfileColumn>({
      columnId: "distinct",
      renderHeaderCell: () => "Distinct",
      renderCell: (c) => (
        <span className={styles.mono}>
          {c.distinct_count.toLocaleString()}
        </span>
      ),
    }),
    createTableColumn<ProfileColumn>({
      columnId: "range",
      renderHeaderCell: () => "Min / Max",
      renderCell: (c) => {
        if (!c.min && !c.max) return <Caption1 className={styles.meta}>—</Caption1>;
        return (
          <span className={styles.mono}>
            {c.min ?? "—"} → {c.max ?? "—"}
          </span>
        );
      },
    }),
    createTableColumn<ProfileColumn>({
      columnId: "mean",
      renderHeaderCell: () => "Mean",
      renderCell: (c) =>
        c.mean === undefined ? (
          <Caption1 className={styles.meta}>—</Caption1>
        ) : (
          <span className={styles.mono}>{c.mean.toFixed(2)}</span>
        ),
    }),
    createTableColumn<ProfileColumn>({
      columnId: "top",
      renderHeaderCell: () => "Top values",
      renderCell: (c) => {
        const tv = c.top_values ?? [];
        if (tv.length === 0)
          return <Caption1 className={styles.meta}>—</Caption1>;
        return (
          <div className={styles.topVals}>
            {tv.slice(0, 4).map((t) => (
              <Tooltip
                key={t.value}
                content={`${t.count.toLocaleString()} rows`}
                relationship="label"
              >
                <Badge appearance="tint" color="informative">
                  {truncate(t.value, 20)}
                </Badge>
              </Tooltip>
            ))}
          </div>
        );
      },
    }),
  ];

  return (
    <DataGrid
      items={columns}
      columns={cols}
      getRowId={(c) => c.name}
      focusMode="composite"
      size="small"
      columnSizingOptions={{
        name: { minWidth: 200, defaultWidth: 240 },
        nulls: { minWidth: 180, defaultWidth: 200 },
        distinct: { minWidth: 100, defaultWidth: 120 },
        range: { minWidth: 180, defaultWidth: 240 },
        mean: { minWidth: 100, defaultWidth: 120 },
        top: { minWidth: 220, defaultWidth: 300 },
      }}
    >
      <DataGridHeader>
        <DataGridRow>
          {({ renderHeaderCell }) => (
            <DataGridHeaderCell>{renderHeaderCell()}</DataGridHeaderCell>
          )}
        </DataGridRow>
      </DataGridHeader>
      <DataGridBody<ProfileColumn>>
        {({ item, rowId }) => (
          <DataGridRow<ProfileColumn> key={rowId}>
            {({ renderCell }) => (
              <DataGridCell>{renderCell(item)}</DataGridCell>
            )}
          </DataGridRow>
        )}
      </DataGridBody>
    </DataGrid>
  );
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n - 1) + "…";
}
