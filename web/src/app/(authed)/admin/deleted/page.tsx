"use client";

import { useMemo, useState } from "react";
import {
  Body1,
  Button,
  Dropdown,
  Field,
  Option,
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
import { ArrowUndo16Regular, Delete16Regular } from "@fluentui/react-icons";
import { useDeleted, usePrincipal, usePurgeRecord, useRestoreRecord } from "@/lib/hooks";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { Paginator } from "@/components/paginator";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "20px" },
  toolbar: { display: "flex", gap: "16px", alignItems: "flex-end" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
  actions: { display: "flex", gap: "8px" },
  superOnly: { color: "#8E1B1B", fontSize: "11px", fontStyle: "italic" },
});

const TYPES = ["", "asset", "pipeline", "check", "policy"] as const;

export default function DeletedPage() {
  const styles = useStyles();
  const { principal } = usePrincipal();
  const isSuperAdmin = (principal?.roles ?? []).includes("super_admin");

  const [type, setType] = useState<string>("");
  const { data, isLoading, error } = useDeleted({
    resourceType: type || undefined,
    limit: 200,
  });
  const restore = useRestoreRecord();
  const purge = usePurgeRecord();
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);

  const total = data?.length ?? 0;
  const pageRows = useMemo(() => {
    if (!data) return [];
    const start = page * pageSize;
    return data.slice(start, start + pageSize);
  }, [data, page, pageSize]);

  return (
    <div className={styles.root}>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        <Title2>Recycle bin</Title2>
        <Body1>
          Every deleted record is captured here. Restore at any time. Only a
          super_admin can permanently delete a tombstone.
        </Body1>
      </div>

      <div className={styles.toolbar}>
        <Field label="Resource type">
          <Dropdown
            value={type || "all"}
            selectedOptions={[type]}
            onOptionSelect={(_, d) => setType(d.optionValue ?? "")}
            style={{ width: 220 }}
          >
            {TYPES.map((t) => (
              <Option key={t || "all"} value={t}>
                {t || "all"}
              </Option>
            ))}
          </Dropdown>
        </Field>
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorBanner error={error} />}
      {data && data.length === 0 && (
        <EmptyState
          title="Recycle bin is empty"
          body="Deleted records appear here with the option to restore."
        />
      )}

      {data && data.length > 0 && (
        <div
          style={{
            backgroundColor: tokens.colorNeutralBackground1,
            borderRadius: "6px",
            boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
            overflow: "hidden",
          }}
        >
        <Table aria-label="Recycle bin">
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Type</TableHeaderCell>
              <TableHeaderCell>Resource</TableHeaderCell>
              <TableHeaderCell>Deleted by</TableHeaderCell>
              <TableHeaderCell>Reason</TableHeaderCell>
              <TableHeaderCell>When</TableHeaderCell>
              <TableHeaderCell>Actions</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pageRows.map((rec) => (
              <TableRow key={rec.ID}>
                <TableCell className={styles.mono}>{rec.ResourceType}</TableCell>
                <TableCell className={styles.mono}>
                  {rec.ResourceID.slice(0, 12)}
                </TableCell>
                <TableCell>
                  <Text className={styles.meta}>
                    {rec.DeletedKind}:{rec.DeletedBy || "—"}
                  </Text>
                </TableCell>
                <TableCell>
                  <Text className={styles.meta}>{rec.DeletionReason}</Text>
                </TableCell>
                <TableCell>
                  <Text className={styles.meta}>
                    {new Date(rec.DeletedAt).toLocaleString()}
                  </Text>
                </TableCell>
                <TableCell>
                  <div className={styles.actions}>
                    <Button
                      size="small"
                      icon={<ArrowUndo16Regular />}
                      onClick={() => restore.mutate(rec.ID)}
                      disabled={restore.isPending}
                    >
                      Restore
                    </Button>
                    <Button
                      size="small"
                      appearance="subtle"
                      icon={<Delete16Regular />}
                      onClick={() => purge.mutate(rec.ID)}
                      disabled={!isSuperAdmin || purge.isPending}
                      title={
                        isSuperAdmin
                          ? "Permanently delete (super_admin)"
                          : "Only super_admin can permanently delete"
                      }
                    >
                      Purge
                    </Button>
                  </div>
                  {!isSuperAdmin && (
                    <span className={styles.superOnly}>
                      super_admin required to purge
                    </span>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <Paginator
          total={total}
          page={page}
          pageSize={pageSize}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
        </div>
      )}

      {(restore.error || purge.error) && (
        <ErrorBanner error={restore.error ?? purge.error} />
      )}
    </div>
  );
}
