"use client";

import Link from "next/link";
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  DataGrid,
  DataGridBody,
  DataGridCell,
  DataGridHeader,
  DataGridHeaderCell,
  DataGridRow,
  TableColumnDefinition,
  createTableColumn,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { api } from "@/lib/api";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  grid: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
  },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
  },
  link: { textDecoration: "none", color: tokens.colorBrandForeground1 },
});

interface ColumnRow {
  id: string;
  name: string;
  qualified_name: string;
  ordinal: number;
  data_type: string;
  nullable: boolean;
  tags: string[];
  description: string;
}

function tagColor(tag: string): "informative" | "danger" | "warning" | "success" | "subtle" {
  if (tag.startsWith("class:phi") || tag.startsWith("class:pci")) return "danger";
  if (tag.startsWith("class:pii") || tag.startsWith("class:secret")) return "warning";
  return "informative";
}

export function SchemaTab({ assetId }: { assetId: string }) {
  const styles = useStyles();
  // Use the lineage endpoint to find children: edges of kind=defines that
  // point downstream from this asset are its columns.
  const lineage = useQuery({
    queryKey: ["children", assetId],
    queryFn: () => api.children(assetId),
  });

  const rows = useMemo<ColumnRow[]>(() => {
    if (!lineage.data) return [];
    const neighbors = (lineage.data as any).neighbors as any[] ?? [];
    return neighbors
      .filter((n) => n.type === "column")
      .map<ColumnRow>((n) => {
        const props = (n.properties ?? {}) as Record<string, any>;
        return {
          id: n.id,
          name: n.name,
          qualified_name: n.qualified_name,
          ordinal: Number(props.ordinal_pos ?? 0),
          data_type: String(props.data_type ?? ""),
          nullable: Boolean(props.nullable),
          tags: n.tags ?? [],
          description: n.description ?? "",
        };
      })
      .sort((a, b) => a.ordinal - b.ordinal);
  }, [lineage.data]);

  const columns = useMemo<TableColumnDefinition<ColumnRow>[]>(
    () => [
      createTableColumn<ColumnRow>({
        columnId: "ordinal",
        compare: (a, b) => a.ordinal - b.ordinal,
        renderHeaderCell: () => "#",
        renderCell: (item) => (
          <span className={styles.mono} style={{ color: tokens.colorNeutralForeground3 }}>
            {item.ordinal}
          </span>
        ),
      }),
      createTableColumn<ColumnRow>({
        columnId: "name",
        compare: (a, b) => a.name.localeCompare(b.name),
        renderHeaderCell: () => "Column",
        renderCell: (item) => (
          <Link
            href={`/asset/${encodeURIComponent(item.qualified_name)}`}
            className={styles.link}
          >
            <span style={{ fontWeight: 600 }}>{item.name}</span>
          </Link>
        ),
      }),
      createTableColumn<ColumnRow>({
        columnId: "type",
        compare: (a, b) => a.data_type.localeCompare(b.data_type),
        renderHeaderCell: () => "Type",
        renderCell: (item) => (
          <span className={styles.mono}>{item.data_type || "—"}</span>
        ),
      }),
      createTableColumn<ColumnRow>({
        columnId: "nullable",
        renderHeaderCell: () => "Nullable",
        renderCell: (item) => (
          <span style={{ fontSize: 12, color: tokens.colorNeutralForeground3 }}>
            {item.nullable ? "yes" : "no"}
          </span>
        ),
      }),
      createTableColumn<ColumnRow>({
        columnId: "tags",
        renderHeaderCell: () => "Classifications",
        renderCell: (item) => (
          <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
            {item.tags.map((t) => (
              <Badge key={t} appearance="filled" color={tagColor(t)}>
                {t.replace(/^class:/, "")}
              </Badge>
            ))}
          </div>
        ),
      }),
    ],
    [styles],
  );

  if (lineage.isLoading) return <LoadingState />;
  if (lineage.error) return <ErrorBanner error={lineage.error} />;
  if (rows.length === 0) {
    return (
      <EmptyState
        title="No columns"
        body="Either this isn't a table/view, or the crawler hasn't introspected it yet."
      />
    );
  }

  return (
    <div className={styles.grid}>
      <DataGrid
        items={rows}
        columns={columns}
        sortable
        getRowId={(item) => item.id}
        size="small"
      >
        <DataGridHeader>
          <DataGridRow>
            {({ renderHeaderCell }) => (
              <DataGridHeaderCell>{renderHeaderCell()}</DataGridHeaderCell>
            )}
          </DataGridRow>
        </DataGridHeader>
        <DataGridBody<ColumnRow>>
          {({ item, rowId }) => (
            <DataGridRow<ColumnRow> key={rowId}>
              {({ renderCell }) => (
                <DataGridCell>{renderCell(item)}</DataGridCell>
              )}
            </DataGridRow>
          )}
        </DataGridBody>
      </DataGrid>
    </div>
  );
}
