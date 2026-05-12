"use client";

import { useMemo, useState } from "react";
import {
  Body1,
  Button,
  Field,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  Title2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { ArrowDownload16Regular } from "@fluentui/react-icons";
import { useAuditFeed, type AuditEvent } from "@/lib/hooks";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { Paginator } from "@/components/paginator";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  toolbar: {
    display: "flex",
    gap: "16px",
    alignItems: "flex-end",
    justifyContent: "space-between",
    flexWrap: "wrap",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
});

function toCSV(events: AuditEvent[]): string {
  const header = [
    "created_at",
    "actor_kind",
    "actor_id",
    "action",
    "resource_type",
    "resource_id",
    "ip",
    "request_id",
  ].join(",");
  const rows = events.map((e) =>
    [
      e.created_at,
      e.actor_kind,
      e.actor_id,
      e.action,
      e.resource_type,
      e.resource_id,
      e.ip ?? "",
      e.request_id ?? "",
    ]
      .map((v) => `"${String(v).replace(/"/g, '""')}"`)
      .join(","),
  );
  return [header, ...rows].join("\n");
}

function downloadCSV(events: AuditEvent[]) {
  const blob = new Blob([toCSV(events)], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `plowered-audit-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

export default function AuditPage() {
  const styles = useStyles();
  const { data, isLoading, error } = useAuditFeed(500);
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(50);

  const filtered = useMemo(() => {
    if (!data) return [];
    if (!filter.trim()) return data;
    const q = filter.toLowerCase();
    return data.filter((e) =>
      [e.action, e.actor_id, e.resource_type, e.resource_id, e.actor_kind]
        .filter(Boolean)
        .some((v) => v.toLowerCase().includes(q)),
    );
  }, [data, filter]);

  const pageRows = useMemo(() => {
    const start = page * pageSize;
    return filtered.slice(start, start + pageSize);
  }, [filtered, page, pageSize]);

  return (
    <div className={styles.root}>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <Title2>Audit log</Title2>
        <Body1>
          Append-only record of every authenticated mutation. Polls every
          15 seconds.
        </Body1>
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorBanner error={error} />}

      {data && (
        <>
          <div className={styles.toolbar}>
            <Field label="Filter">
              <Input
                value={filter}
                onChange={(_, d) => setFilter(d.value)}
                placeholder="action, actor, resource…"
                style={{ width: 320 }}
              />
            </Field>
            <Button
              icon={<ArrowDownload16Regular />}
              onClick={() => downloadCSV(filtered)}
              disabled={filtered.length === 0}
            >
              Export {filtered.length} as CSV
            </Button>
          </div>

          {filtered.length === 0 ? (
            <EmptyState
              title="No matching events"
              body={
                filter
                  ? "Try clearing the filter — there are events but none match."
                  : "Audit events are written automatically on every mutation."
              }
            />
          ) : (
            <div
              style={{
                backgroundColor: tokens.colorNeutralBackground1,
                borderRadius: "6px",
                boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
                overflow: "hidden",
              }}
            >
            <Table aria-label="Audit events">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>When</TableHeaderCell>
                  <TableHeaderCell>Actor</TableHeaderCell>
                  <TableHeaderCell>Action</TableHeaderCell>
                  <TableHeaderCell>Resource</TableHeaderCell>
                  <TableHeaderCell>Request</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageRows.map((e) => (
                  <TableRow key={e.event_id}>
                    <TableCell>
                      <Text className={styles.meta}>
                        {new Date(e.created_at).toLocaleString()}
                      </Text>
                    </TableCell>
                    <TableCell className={styles.mono}>
                      {e.actor_kind}:{e.actor_id || "—"}
                    </TableCell>
                    <TableCell className={styles.mono}>{e.action}</TableCell>
                    <TableCell className={styles.mono}>
                      {e.resource_type}/{e.resource_id || "—"}
                    </TableCell>
                    <TableCell>
                      <Text className={styles.meta}>{e.request_id ?? ""}</Text>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <Paginator
              total={filtered.length}
              page={page}
              pageSize={pageSize}
              onPageChange={setPage}
              onPageSizeChange={setPageSize}
            />
            </div>
          )}
        </>
      )}
    </div>
  );
}
