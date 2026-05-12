"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Button,
  DataGrid,
  DataGridBody,
  DataGridCell,
  DataGridHeader,
  DataGridHeaderCell,
  DataGridRow,
  Input,
  TableColumnDefinition,
  TabList,
  Tab,
  Toolbar,
  ToolbarButton,
  ToolbarDivider,
  Tooltip,
  createTableColumn,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  Database20Regular,
  Search20Regular,
  Filter20Regular,
  ShieldCheckmark16Regular,
} from "@fluentui/react-icons";
import { api } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { Paginator } from "@/components/paginator";
import { Truncate } from "@/components/truncate";

interface CatalogAsset {
  id: string;
  qualified_name: string;
  type: string;
  name: string;
  description?: string;
  trust?: string;
  tags?: string[];
  owners?: string[];
  properties?: Record<string, any>;
  updated_at?: string;
  created_at?: string;
}

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "12px" },
  toolbar: {
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    padding: "4px 8px",
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    alignItems: "center",
  },
  search: { width: "min(420px, 60vw)" },
  grid: {
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    overflow: "hidden",
  },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorNeutralForeground2,
  },
  tagRow: { display: "flex", flexWrap: "wrap", gap: "4px" },
  rowName: {
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    textDecoration: "none",
    color: "inherit",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  countBadge: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
  },
});

const TYPE_TABS: { key: string; label: string }[] = [
  { key: "all",       label: "All" },
  { key: "table",     label: "Tables" },
  { key: "view",      label: "Views" },
  { key: "column",    label: "Columns" },
  { key: "schema",    label: "Schemas" },
  { key: "dashboard", label: "Dashboards" },
];

type BadgeColor =
  | "brand" | "danger" | "important" | "informative"
  | "severe" | "subtle" | "success" | "warning";

/**
 * Semantic colour for a classification tag.
 *
 *   secret / phi / pci  → danger (red) — highest-severity, regulated
 *   pii                 → warning (yellow) — important but not regulated
 *   anything else       → informative (blue) — generic
 */
function tagColor(tag: string): BadgeColor {
  if (
    tag.startsWith("class:secret") ||
    tag.startsWith("class:phi") ||
    tag.startsWith("class:pci")
  ) return "danger";
  if (tag.startsWith("class:pii")) return "warning";
  return "informative";
}

/**
 * Asset-type badge colour. Each type gets its own hue so a scrolling
 * list is scannable without reading every label — and quieter types
 * (column, the highest-volume row) intentionally render subtle so
 * they don't drown out the rest of the page in brand orange.
 */
function typeColor(type: string): BadgeColor {
  switch (type) {
    case "table":     return "informative";
    case "view":      return "success";
    case "schema":    return "important";
    case "dashboard": return "brand";
    case "model":     return "severe";
    case "pipeline":  return "brand";
    case "column":    return "subtle";
    default:          return "subtle";
  }
}

export default function CatalogPage() {
  const styles = useStyles();
  const [type, setType] = useState("all");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(50);

  const list = useQuery({
    queryKey: ["assets", "catalog", type],
    queryFn: () =>
      api.listAssets({
        pageSize: 500,
        type: type === "all" ? undefined : type,
      }),
  });

  const filtered = useMemo<CatalogAsset[]>(() => {
    const items = (list.data?.assets ?? []) as CatalogAsset[];
    if (!q) return items;
    const needle = q.toLowerCase();
    return items.filter(
      (a) =>
        a.name.toLowerCase().includes(needle) ||
        a.qualified_name.toLowerCase().includes(needle) ||
        (a.tags ?? []).some((t) => t.toLowerCase().includes(needle)),
    );
  }, [list.data, q]);

  // Counts per type for the tab badges (computed off the unfiltered total).
  const typeCounts = useMemo(() => {
    const items = (list.data?.assets ?? []) as CatalogAsset[];
    const byType = new Map<string, number>();
    for (const a of items) byType.set(a.type, (byType.get(a.type) ?? 0) + 1);
    return byType;
  }, [list.data]);

  const total = (list.data?.assets ?? []).length;

  const columns = useMemo<TableColumnDefinition<CatalogAsset>[]>(
    () => [
      createTableColumn<CatalogAsset>({
        columnId: "name",
        compare: (a, b) => a.name.localeCompare(b.name),
        renderHeaderCell: () => "Name",
        renderCell: (item) => (
          <Link
            href={`/asset/${encodeURIComponent(item.qualified_name)}`}
            className={styles.rowName}
            style={{ display: "block", minWidth: 0, maxWidth: "100%" }}
          >
            <Truncate text={item.name} style={{ fontWeight: 600 }} />
            <Truncate text={item.qualified_name} className={styles.mono} />
          </Link>
        ),
      }),
      createTableColumn<CatalogAsset>({
        columnId: "type",
        compare: (a, b) => a.type.localeCompare(b.type),
        renderHeaderCell: () => "Type",
        renderCell: (item) => (
          <Badge appearance="outline" color={typeColor(item.type)}>
            {item.type}
          </Badge>
        ),
      }),
      createTableColumn<CatalogAsset>({
        columnId: "tags",
        renderHeaderCell: () => "Tags",
        renderCell: (item) => {
          const tags = item.tags ?? [];
          if (tags.length === 0) {
            return <span className={styles.meta}>—</span>;
          }
          return (
            <div className={styles.tagRow}>
              {tags.slice(0, 4).map((t) => (
                <Badge key={t} appearance="filled" color={tagColor(t)}>
                  {t.replace(/^class:/, "")}
                </Badge>
              ))}
              {tags.length > 4 && (
                <Tooltip content={tags.slice(4).join(", ")} relationship="label">
                  <Badge appearance="ghost" color="subtle">
                    +{tags.length - 4}
                  </Badge>
                </Tooltip>
              )}
            </div>
          );
        },
      }),
      createTableColumn<CatalogAsset>({
        columnId: "trust",
        compare: (a, b) =>
          (a.trust ?? "").localeCompare(b.trust ?? ""),
        renderHeaderCell: () => "Trust",
        renderCell: (item) => {
          const trust = item.trust ?? "unverified";
          const isCertified = trust === "certified" || trust === "reviewed";
          return (
            <Badge
              appearance={isCertified ? "filled" : "outline"}
              color={
                trust === "certified"
                  ? "success"
                  : trust === "reviewed"
                    ? "informative"
                    : trust === "deprecated"
                      ? "danger"
                      : "subtle"
              }
              icon={isCertified ? <ShieldCheckmark16Regular /> : undefined}
            >
              {trust}
            </Badge>
          );
        },
      }),
      createTableColumn<CatalogAsset>({
        columnId: "updated",
        compare: (a, b) =>
          (a.updated_at ?? "").localeCompare(b.updated_at ?? ""),
        renderHeaderCell: () => "Updated",
        renderCell: (item) => (
          <span className={styles.meta}>
            {item.updated_at
              ? new Date(item.updated_at).toLocaleString()
              : "—"}
          </span>
        ),
      }),
    ],
    [styles],
  );

  return (
    <>
      <PageHeader
        title="Catalog"
        subtitle="Every catalogued resource: tables, views, columns, dashboards. Sort, filter, drill in."
        crumbs={[{ label: "Home", href: "/" }, { label: "Catalog" }]}
        actions={
          <>
            <Button icon={<ArrowSync20Regular />} onClick={() => list.refetch()}>
              Refresh
            </Button>
            <Link href="/connections">
              <Button appearance="primary" icon={<Database20Regular />}>
                Connect a source
              </Button>
            </Link>
          </>
        }
      />

      <div className={styles.body}>
        <TabList
          selectedValue={type}
          onTabSelect={(_, d) => setType(String(d.value))}
        >
          {TYPE_TABS.map((t) => {
            const c = t.key === "all" ? total : typeCounts.get(t.key) ?? 0;
            return (
              <Tab key={t.key} value={t.key}>
                {t.label}
                <Badge
                  appearance="ghost"
                  color="subtle"
                  className={styles.countBadge}
                  style={{ marginLeft: 6 }}
                >
                  {c}
                </Badge>
              </Tab>
            );
          })}
        </TabList>

        <Toolbar size="small" className={styles.toolbar}>
          <Input
            className={styles.search}
            contentBefore={<Search20Regular />}
            placeholder="Filter by name, qualified name, or tag…"
            value={q}
            onChange={(_, d) => setQ(d.value)}
          />
          <ToolbarDivider />
          <ToolbarButton
            appearance="subtle"
            icon={<Filter20Regular />}
            disabled
          >
            More filters (coming soon)
          </ToolbarButton>
          <div style={{ flex: 1 }} />
          <span className={styles.meta}>
            {filtered.length} of {total} assets
          </span>
        </Toolbar>

        {list.isLoading && <LoadingState />}
        {list.error && <ErrorBanner error={list.error} />}
        {!list.isLoading && filtered.length === 0 && (
          <EmptyState
            title="No assets match"
            body={
              q
                ? "Try a different filter, or clear the search."
                : "Wire a connection and click Crawl to populate the catalog."
            }
          />
        )}

        {filtered.length > 0 && (
          <div className={styles.grid}>
            <DataGrid
              items={filtered.slice(page * pageSize, (page + 1) * pageSize)}
              columns={columns}
              sortable
              resizableColumns
              getRowId={(item) => item.id}
              focusMode="composite"
              size="small"
              columnSizingOptions={{
                name:    { minWidth: 280, defaultWidth: 420 },
                type:    { minWidth: 100, defaultWidth: 110 },
                tags:    { minWidth: 200, defaultWidth: 260 },
                trust:   { minWidth: 120, defaultWidth: 140 },
                updated: { minWidth: 140, defaultWidth: 180 },
              }}
            >
              <DataGridHeader>
                <DataGridRow>
                  {({ renderHeaderCell }) => (
                    <DataGridHeaderCell>{renderHeaderCell()}</DataGridHeaderCell>
                  )}
                </DataGridRow>
              </DataGridHeader>
              <DataGridBody<CatalogAsset>>
                {({ item, rowId }) => (
                  <DataGridRow<CatalogAsset> key={rowId}>
                    {({ renderCell }) => (
                      <DataGridCell>{renderCell(item)}</DataGridCell>
                    )}
                  </DataGridRow>
                )}
              </DataGridBody>
            </DataGrid>
            <Paginator
              total={filtered.length}
              page={page}
              pageSize={pageSize}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
          </div>
        )}
      </div>
    </>
  );
}
