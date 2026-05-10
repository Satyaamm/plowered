"use client";

import { useMemo, useState } from "react";
import {
  Badge,
  Button,
  DataGrid,
  DataGridBody,
  DataGridCell,
  DataGridHeader,
  DataGridHeaderCell,
  DataGridRow,
  Menu,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  TableColumnDefinition,
  Toolbar,
  ToolbarButton,
  ToolbarDivider,
  createTableColumn,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  ArrowSync20Regular,
  Delete20Regular,
  MoreVertical20Regular,
  Play20Regular,
  Search20Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { ConnectionWizard } from "@/components/connection-wizard";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  Connection,
  useConnections,
  useClassifyConnection,
  useCrawlConnection,
  useDeleteConnection,
  useTestConnection,
} from "@/lib/hooks";
import { useJob } from "@/lib/hooks/use-jobs";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "16px" },
  toolbar: {
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    padding: "4px 8px",
  },
  grid: {
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    overflow: "hidden",
  },
  mono: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  actionRow: { display: "flex", gap: "4px", alignItems: "center" },
});

function healthBadge(h: Connection["health"]) {
  switch (h) {
    case "healthy":
      return <Badge appearance="filled" color="success">healthy</Badge>;
    case "degraded":
      return <Badge appearance="filled" color="warning">degraded</Badge>;
    case "unreachable":
      return <Badge appearance="filled" color="danger">unreachable</Badge>;
    default:
      return <Badge appearance="outline" color="subtle">unknown</Badge>;
  }
}

export default function ConnectionsPage() {
  const styles = useStyles();
  const list = useConnections();
  const test = useTestConnection();
  const del = useDeleteConnection();
  const crawl = useCrawlConnection();
  const classify = useClassifyConnection();
  const classifyJobId =
    classify.data && "job_id" in classify.data ? classify.data.job_id : null;
  const classifyJob = useJob(classifyJobId);
  const [drawerOpen, setDrawerOpen] = useState(false);

  const columns = useMemo<TableColumnDefinition<Connection>[]>(
    () => [
      createTableColumn<Connection>({
        columnId: "name",
        compare: (a, b) => a.name.localeCompare(b.name),
        renderHeaderCell: () => "Name",
        renderCell: (item) => (
          <span style={{ fontWeight: 600 }}>{item.name}</span>
        ),
      }),
      createTableColumn<Connection>({
        columnId: "type",
        compare: (a, b) => a.type.localeCompare(b.type),
        renderHeaderCell: () => "Type",
        renderCell: (item) => (
          <Badge appearance="outline" color="brand">{item.type}</Badge>
        ),
      }),
      createTableColumn<Connection>({
        columnId: "host",
        renderHeaderCell: () => "Host",
        renderCell: (item) => {
          const cfg = item.config as any;
          return (
            <span className={styles.mono}>
              {cfg?.host}:{cfg?.port ?? 5432}/{cfg?.database}
            </span>
          );
        },
      }),
      createTableColumn<Connection>({
        columnId: "health",
        compare: (a, b) => a.health.localeCompare(b.health),
        renderHeaderCell: () => "Health",
        renderCell: (item) => healthBadge(item.health),
      }),
      createTableColumn<Connection>({
        columnId: "checked",
        renderHeaderCell: () => "Last check",
        renderCell: (item) => (
          <span className={styles.meta}>
            {item.last_check_at
              ? new Date(item.last_check_at).toLocaleString()
              : "never"}
          </span>
        ),
      }),
      createTableColumn<Connection>({
        columnId: "actions",
        renderHeaderCell: () => "",
        renderCell: (item) => (
          <div className={styles.actionRow}>
            <Button
              size="small"
              appearance="subtle"
              icon={<Play20Regular />}
              onClick={() => test.mutate(item.id)}
              disabled={test.isPending}
            >
              Test
            </Button>
            <Button
              size="small"
              appearance="subtle"
              icon={<Search20Regular />}
              onClick={() => crawl.mutate(item.id)}
              disabled={crawl.isPending}
            >
              Crawl
            </Button>
            <Menu>
              <MenuTrigger disableButtonEnhancement>
                <Button
                  size="small"
                  appearance="subtle"
                  icon={<MoreVertical20Regular />}
                  aria-label="More actions"
                />
              </MenuTrigger>
              <MenuPopover>
                <MenuList>
                  <MenuItem
                    onClick={() => classify.mutate(item.id)}
                  >
                    Classify (sample data)
                  </MenuItem>
                  <MenuItem
                    icon={<Delete20Regular />}
                    onClick={() => del.mutate(item.id)}
                  >
                    Delete
                  </MenuItem>
                </MenuList>
              </MenuPopover>
            </Menu>
          </div>
        ),
      }),
    ],
    [styles, test, del, crawl, classify],
  );

  return (
    <>
      <PageHeader
        title="Connections"
        subtitle="Datasources Plowered talks to. Credentials are encrypted with AES-256-GCM."
        crumbs={[{ label: "Home", href: "/" }, { label: "Management" }, { label: "Connections" }]}
        actions={
          <>
            <Button icon={<ArrowSync20Regular />} onClick={() => list.refetch()}>
              Refresh
            </Button>
            <Button
              appearance="primary"
              icon={<Add20Regular />}
              onClick={() => setDrawerOpen(true)}
            >
              New connection
            </Button>
          </>
        }
      />

      <div className={styles.body}>
        <Toolbar size="small" className={styles.toolbar}>
          <ToolbarButton
            appearance="subtle"
            icon={<Add20Regular />}
            onClick={() => setDrawerOpen(true)}
          >
            Add
          </ToolbarButton>
          <ToolbarDivider />
          <ToolbarButton
            appearance="subtle"
            icon={<ArrowSync20Regular />}
            onClick={() => list.refetch()}
          >
            Refresh
          </ToolbarButton>
        </Toolbar>

        {list.isLoading && <LoadingState />}
        {list.error && <ErrorBanner error={list.error} />}
        {test.error && <ErrorBanner error={test.error} />}
        {crawl.isSuccess && (
          <div
            style={{
              padding: "10px 12px",
              borderRadius: "6px",
              background: "#FBF1EB",
              color: "#552B0E",
              fontSize: 13,
            }}
          >
            Crawl queued — assets appear in the Catalog within seconds.
          </div>
        )}
        {classify.isSuccess && classify.data && !("job_id" in classify.data) && (
          <div
            style={{
              padding: "10px 12px",
              borderRadius: "6px",
              background: "#FBF1EB",
              color: "#552B0E",
              fontSize: 13,
            }}
          >
            Classified {classify.data.tables} tables /{" "}
            {classify.data.columns} columns — {classify.data.tagged} columns
            now carry auto-detected tags.
          </div>
        )}
        {classifyJob.data && classifyJob.data.status !== "succeeded" && classifyJob.data.status !== "failed" && (
          <div
            style={{
              padding: "10px 12px",
              borderRadius: "6px",
              background: "#FBF1EB",
              color: "#552B0E",
              fontSize: 13,
            }}
          >
            Classifying… {classifyJob.data.progress_pct}%
            {classifyJob.data.message ? ` — ${classifyJob.data.message}` : ""}
          </div>
        )}
        {classifyJob.data?.status === "succeeded" && classifyJob.data.result && (
          <div
            style={{
              padding: "10px 12px",
              borderRadius: "6px",
              background: "#FBF1EB",
              color: "#552B0E",
              fontSize: 13,
            }}
          >
            Classified{" "}
            {(classifyJob.data.result as Record<string, number>).tables ?? 0}{" "}
            tables /{" "}
            {(classifyJob.data.result as Record<string, number>).columns ?? 0}{" "}
            columns —{" "}
            {(classifyJob.data.result as Record<string, number>).tagged ?? 0}{" "}
            columns now carry auto-detected tags.
          </div>
        )}
        {classifyJob.data?.status === "failed" && (
          <div
            style={{
              padding: "10px 12px",
              borderRadius: "6px",
              background: "#FBE9E9",
              color: "#5C1F1F",
              fontSize: 13,
            }}
          >
            Classification failed: {classifyJob.data.error ?? "unknown error"}
          </div>
        )}
        {classify.isPending && (
          <div style={{ padding: "10px 12px", fontSize: 13, color: "#552B0E" }}>
            Queueing classification…
          </div>
        )}
        {list.data && list.data.length === 0 && !list.isLoading && (
          <EmptyState
            title="No connections yet"
            body="Wire your first datasource to start populating the catalog."
          />
        )}

        {list.data && list.data.length > 0 && (
          <div className={styles.grid}>
            <DataGrid
              items={list.data}
              columns={columns}
              sortable
              getRowId={(item) => item.id}
              focusMode="composite"
            >
              <DataGridHeader>
                <DataGridRow>
                  {({ renderHeaderCell }) => (
                    <DataGridHeaderCell>{renderHeaderCell()}</DataGridHeaderCell>
                  )}
                </DataGridRow>
              </DataGridHeader>
              <DataGridBody<Connection>>
                {({ item, rowId }) => (
                  <DataGridRow<Connection> key={rowId}>
                    {({ renderCell }) => (
                      <DataGridCell>{renderCell(item)}</DataGridCell>
                    )}
                  </DataGridRow>
                )}
              </DataGridBody>
            </DataGrid>
          </div>
        )}
      </div>

      <ConnectionWizard open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </>
  );
}
