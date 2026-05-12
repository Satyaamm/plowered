"use client";

import { useMemo, useState } from "react";
import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Field,
  Input,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Add20Regular, Gavel20Regular, Open20Regular } from "@fluentui/react-icons";
import {
  useIssueLegalHold,
  useLegalHolds,
  useReleaseLegalHold,
} from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { Paginator } from "@/components/paginator";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "16px" },
  tableWrap: {
    backgroundColor: tokens.colorNeutralBackground1,
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
  },
  scope: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "12px",
    color: tokens.colorNeutralForeground2,
  },
  formStack: { display: "flex", flexDirection: "column", gap: "12px", minWidth: "420px" },
});

export default function LegalHoldsPage() {
  const styles = useStyles();
  const { data, isLoading, error } = useLegalHolds();
  const issue = useIssueLegalHold();
  const release = useReleaseLegalHold();
  const [open, setOpen] = useState(false);

  const [matter, setMatter] = useState("");
  const [reason, setReason] = useState("");
  const [resourceTypes, setResourceTypes] = useState("");

  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);
  const total = data?.length ?? 0;
  const pageRows = useMemo(() => {
    if (!data) return [];
    const start = page * pageSize;
    return data.slice(start, start + pageSize);
  }, [data, page, pageSize]);

  const onIssue = async () => {
    await issue.mutateAsync({
      Matter: matter,
      Reason: reason,
      Scope: resourceTypes
        ? { resource_types: resourceTypes.split(",").map((s) => s.trim()).filter(Boolean) }
        : {},
    });
    setOpen(false);
    setMatter("");
    setReason("");
    setResourceTypes("");
  };

  return (
    <>
      <PageHeader
        title="Legal holds"
        subtitle="Block deletion of resources during litigation. Active holds enforce 409 on every delete."
        crumbs={[{ label: "Home", href: "/" }, { label: "Compliance" }, { label: "Legal holds" }]}
        actions={
          <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
            <DialogTrigger disableButtonEnhancement>
              <Button appearance="primary" icon={<Add20Regular />}>
                Issue hold
              </Button>
            </DialogTrigger>
            <DialogSurface>
              <DialogBody>
                <DialogTitle>Issue legal hold</DialogTitle>
                <DialogContent>
                  <div className={styles.formStack}>
                    <Field label="Matter" required>
                      <Input value={matter} onChange={(_, d) => setMatter(d.value)} placeholder="Acme v Plowered #2026-04" />
                    </Field>
                    <Field label="Reason">
                      <Textarea value={reason} onChange={(_, d) => setReason(d.value)} rows={2} />
                    </Field>
                    <Field
                      label="Scope: resource types"
                      hint="Comma-separated. Leave empty for tenant-wide hold."
                    >
                      <Input
                        value={resourceTypes}
                        onChange={(_, d) => setResourceTypes(d.value)}
                        placeholder="pipeline, check, asset"
                      />
                    </Field>
                  </div>
                </DialogContent>
                <DialogActions>
                  <DialogTrigger disableButtonEnhancement>
                    <Button appearance="secondary">Cancel</Button>
                  </DialogTrigger>
                  <Button
                    appearance="primary"
                    disabled={!matter || issue.isPending}
                    onClick={onIssue}
                  >
                    Issue hold
                  </Button>
                </DialogActions>
              </DialogBody>
            </DialogSurface>
          </Dialog>
        }
      />

      <div className={styles.body}>
        {isLoading && <LoadingState />}
        {error && <ErrorBanner error={error} />}
        {data && data.length === 0 && (
          <EmptyState
            title="No legal holds in place"
            body="Issue a hold to block delete on resources caught by litigation or regulator request."
          />
        )}

        {data && data.length > 0 && (
          <div className={styles.tableWrap}>
            <Table aria-label="Legal holds">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Status</TableHeaderCell>
                  <TableHeaderCell>Matter</TableHeaderCell>
                  <TableHeaderCell>Scope</TableHeaderCell>
                  <TableHeaderCell>Issued</TableHeaderCell>
                  <TableHeaderCell>Issued by</TableHeaderCell>
                  <TableHeaderCell style={{ width: 130 }}>Actions</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageRows.map((h) => {
                  const released = h.ReleasedAt && h.ReleasedAt !== "0001-01-01T00:00:00Z";
                  return (
                    <TableRow key={h.ID}>
                      <TableCell>
                        {released ? (
                          <Badge appearance="outline" color="subtle">released</Badge>
                        ) : (
                          <Badge appearance="filled" color="danger" icon={<Gavel20Regular />}>
                            active
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>{h.Matter}</TableCell>
                      <TableCell>
                        <span className={styles.scope}>
                          {Object.keys(h.Scope ?? {}).length === 0
                            ? "tenant-wide"
                            : JSON.stringify(h.Scope)}
                        </span>
                      </TableCell>
                      <TableCell>
                        <Text size={200}>
                          {new Date(h.IssuedAt).toLocaleString()}
                        </Text>
                      </TableCell>
                      <TableCell>
                        <Text size={200}>{h.IssuedBy}</Text>
                      </TableCell>
                      <TableCell>
                        {!released && (
                          <Button
                            size="small"
                            icon={<Open20Regular />}
                            onClick={() => release.mutate(h.ID)}
                            disabled={release.isPending}
                          >
                            Release
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
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
      </div>
    </>
  );
}
