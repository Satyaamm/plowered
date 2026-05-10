"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Caption1,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { api } from "@/lib/api";
import { LineageGraph } from "@/components/lineage-graph";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

interface ColumnLineageEdge {
  id: string;
  source_asset_id: string;
  source_qualified_name: string;
  source_column: string;
  target_asset_id: string;
  target_qualified_name: string;
  target_column: string;
  transform: string;
  expression?: string;
}

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  panel: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "16px",
    minHeight: "420px",
    display: "flex",
    flexDirection: "column",
    gap: "10px",
  },
  toolbar: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    gap: "12px",
  },
  hint: { color: tokens.colorNeutralForeground3 },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace", fontSize: "12px" },
  expr: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "11px",
    color: tokens.colorNeutralForeground3,
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
    maxWidth: "320px",
  },
});

export function LineageTab({ assetId }: { assetId: string }) {
  const styles = useStyles();
  const [columnsView, setColumnsView] = useState(false);

  const lineage = useQuery({
    queryKey: ["lineage", assetId, "both", 2],
    queryFn: () => api.lineage(assetId, "both", 2),
  });

  const columns = useQuery<{ edges: ColumnLineageEdge[] }>({
    queryKey: ["column-lineage", assetId],
    queryFn: async ({ signal }) => {
      const res = await fetch(
        `/api/v1/assets/${encodeURIComponent(assetId)}/column-lineage`,
        { credentials: "include", signal },
      );
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return res.json();
    },
    enabled: columnsView,
    staleTime: 5_000,
  });

  if (lineage.isLoading) return <LoadingState />;
  if (lineage.error) return <ErrorBanner error={lineage.error} />;

  const data = lineage.data;
  const neighborCount = (data as { neighbors?: unknown[] } | undefined)?.neighbors?.length ?? 0;
  const columnEdges = columns.data?.edges ?? [];

  return (
    <div className={styles.root}>
      <div className={styles.toolbar}>
        <Caption1 className={styles.hint}>
          Showing 2 hops in each direction. Drag nodes to rearrange. Click a node to drill in.
        </Caption1>
        <Switch
          label="Column-level"
          checked={columnsView}
          onChange={(_, d) => setColumnsView(d.checked)}
        />
      </div>

      {!columnsView && (
        neighborCount === 0 ? (
          <EmptyState
            title="No lineage edges"
            body="Lineage gets populated by crawlers, dbt manifest imports, and SQL parsers. None visible yet."
          />
        ) : (
          <div className={styles.panel}>
            <LineageGraph data={data!} />
          </div>
        )
      )}

      {columnsView && (
        <div className={styles.panel} style={{ minHeight: 0 }}>
          {columns.isLoading && <LoadingState />}
          {columns.error && <ErrorBanner error={columns.error} />}
          {!columns.isLoading && !columns.error && columnEdges.length === 0 && (
            <EmptyState
              title="No column-level lineage"
              body="Column edges are produced when a transform_run task succeeds with a SELECT this asset feeds — or runs on top of it."
            />
          )}
          {columnEdges.length > 0 && (
            <Table aria-label="Column lineage">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Source</TableHeaderCell>
                  <TableHeaderCell>Target</TableHeaderCell>
                  <TableHeaderCell>Transform</TableHeaderCell>
                  <TableHeaderCell>Expression</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {columnEdges.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell className={styles.mono}>
                      {shortQN(e.source_qualified_name)}.{e.source_column}
                    </TableCell>
                    <TableCell className={styles.mono}>
                      {shortQN(e.target_qualified_name)}.{e.target_column}
                    </TableCell>
                    <TableCell>
                      <Badge appearance={e.transform === "identity" ? "outline" : "filled"} color="brand">
                        {e.transform}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Text className={styles.expr} title={e.expression ?? ""}>
                        {e.expression ?? "—"}
                      </Text>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      )}
    </div>
  );
}

function shortQN(qn: string): string {
  const parts = qn.split(".");
  if (parts.length <= 3) return qn;
  // Drop the connection prefix for display so the table fits.
  return parts.slice(1, parts.length - 1).join(".");
}
