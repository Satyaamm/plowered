"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import {
  Body1,
  Button,
  Menu,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
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
import { MoreHorizontal20Regular, Add20Regular } from "@fluentui/react-icons";
import { useChecks, useDeleteCheck, useRunCheck, useUpdateCheck } from "@/lib/hooks";
import { CheckDesigner } from "@/components/check-designer";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { Paginator } from "@/components/paginator";
import type { Check } from "@/lib/types-orchestration";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  header: { display: "flex", justifyContent: "space-between", alignItems: "flex-end", gap: "16px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
});

export default function ChecksPage() {
  const styles = useStyles();
  const { data, isLoading, error } = useChecks();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [editing, setEditing] = useState<Check | null>(null);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);

  const total = data?.length ?? 0;
  const pageRows = useMemo(() => {
    if (!data) return [];
    const start = page * pageSize;
    return data.slice(start, start + pageSize);
  }, [data, page, pageSize]);

  const openNew = () => {
    setEditing(null);
    setDrawerOpen(true);
  };
  const openEdit = (c: Check) => {
    setEditing(c);
    setDrawerOpen(true);
  };

  return (
    <div className={styles.root}>
      <div className={styles.header}>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <Title2>Quality checks</Title2>
          <Body1>
            Configurable assertions on assets — row count, freshness, nulls,
            uniqueness, and custom SQL.
          </Body1>
        </div>
        <Button appearance="primary" icon={<Add20Regular />} onClick={openNew}>
          New check
        </Button>
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorBanner error={error} />}
      {data && data.length === 0 && (
        <EmptyState
          title="No checks defined"
          body='Click "New check" to add one — pick an asset, choose a type, set a threshold.'
          action={
            <Button appearance="primary" icon={<Add20Regular />} onClick={openNew}>
              New check
            </Button>
          }
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
        <Table aria-label="Checks">
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Name</TableHeaderCell>
              <TableHeaderCell>Type</TableHeaderCell>
              <TableHeaderCell>Asset</TableHeaderCell>
              <TableHeaderCell>Severity</TableHeaderCell>
              <TableHeaderCell>Enabled</TableHeaderCell>
              <TableHeaderCell>Updated</TableHeaderCell>
              <TableHeaderCell aria-label="actions" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {pageRows.map((c) => (
              <CheckRow key={c.ID} check={c} onEdit={() => openEdit(c)} />
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

      <CheckDesigner
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        existing={editing}
      />
    </div>
  );
}

function CheckRow({ check, onEdit }: { check: Check; onEdit: () => void }) {
  const styles = useStyles();
  const del = useDeleteCheck();
  const update = useUpdateCheck(check.ID);
  const run = useRunCheck();

  return (
    <TableRow>
      <TableCell>
        <Link
          href={`/checks/${encodeURIComponent(check.ID)}`}
          style={{ color: tokens.colorBrandForeground1 }}
        >
          {check.Name}
        </Link>
      </TableCell>
      <TableCell className={styles.mono}>{check.Type}</TableCell>
      <TableCell>
        <Text className={styles.meta}>{check.AssetQN || check.AssetID}</Text>
      </TableCell>
      <TableCell>{check.Severity ?? "warning"}</TableCell>
      <TableCell>{check.Enabled ? "yes" : "no"}</TableCell>
      <TableCell>
        <Text className={styles.meta}>
          {new Date(check.UpdatedAt).toLocaleString()}
        </Text>
      </TableCell>
      <TableCell>
        <Menu>
          <MenuTrigger disableButtonEnhancement>
            <Button appearance="subtle" icon={<MoreHorizontal20Regular />} aria-label="actions" />
          </MenuTrigger>
          <MenuPopover>
            <MenuList>
              <MenuItem onClick={() => run.mutate({ id: check.ID })}>Run now</MenuItem>
              <MenuItem onClick={onEdit}>Edit</MenuItem>
              <MenuItem
                onClick={() =>
                  update.mutate({ ...check, Enabled: !check.Enabled })
                }
              >
                {check.Enabled ? "Disable" : "Enable"}
              </MenuItem>
              <MenuItem
                onClick={() => {
                  if (confirm(`Delete check "${check.Name}"?`)) del.mutate(check.ID);
                }}
              >
                Delete
              </MenuItem>
            </MenuList>
          </MenuPopover>
        </Menu>
      </TableCell>
    </TableRow>
  );
}
