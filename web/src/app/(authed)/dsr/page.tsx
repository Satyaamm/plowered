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
  Dropdown,
  Field,
  Input,
  Option,
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
import { Add20Regular, ShieldKeyhole20Regular } from "@fluentui/react-icons";
import { useCreateDSR, useDSRRequests, useUpdateDSRStatus } from "@/lib/hooks";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import { Paginator } from "@/components/paginator";
import { Truncate } from "@/components/truncate";
import { InfoLabel } from "@/components/info-label";

const TYPES = ["access", "portability", "rectification", "erasure", "restriction"];
const STATUS = ["received", "processing", "completed", "rejected"];

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "16px" },
  tableWrap: {
    backgroundColor: tokens.colorNeutralBackground1,
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    overflow: "hidden",
  },
  formStack: { display: "flex", flexDirection: "column", gap: "12px", minWidth: "420px" },
  due: { fontSize: "12px", color: tokens.colorNeutralForeground3 },
  dueOver: { fontSize: "12px", color: tokens.colorPaletteRedForeground1, fontWeight: 600 },
  mono: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace", fontSize: "12px" },
});

function badgeColor(status: string) {
  switch (status) {
    case "received": return "warning";
    case "processing": return "informative";
    case "completed": return "success";
    case "rejected": return "danger";
    default: return "subtle";
  }
}

export default function DSRPage() {
  const styles = useStyles();
  const { data, isLoading, error } = useDSRRequests();
  const create = useCreateDSR();
  const updateStatus = useUpdateDSRStatus();
  const [open, setOpen] = useState(false);

  const [subject, setSubject] = useState("");
  const [type, setType] = useState("access");
  const [notes, setNotes] = useState("");

  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(25);
  const total = data?.length ?? 0;
  const pageRows = useMemo(() => {
    if (!data) return [];
    const start = page * pageSize;
    return data.slice(start, start + pageSize);
  }, [data, page, pageSize]);

  const onCreate = async () => {
    await create.mutateAsync({ subject_id: subject, type, notes });
    setOpen(false);
    setSubject("");
    setNotes("");
    setType("access");
  };

  const now = Date.now();

  return (
    <>
      <PageHeader
        title="DSR requests"
        subtitle="Data Subject Requests under GDPR Articles 15-20. The 30-day clock starts on receipt."
        crumbs={[{ label: "Home", href: "/" }, { label: "Compliance" }, { label: "DSR requests" }]}
        actions={
          <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
            <DialogTrigger disableButtonEnhancement>
              <Button appearance="primary" icon={<Add20Regular />}>
                New request
              </Button>
            </DialogTrigger>
            <DialogSurface>
              <DialogBody>
                <DialogTitle>File a DSR</DialogTitle>
                <DialogContent>
                  <div className={styles.formStack}>
                    <Field
                      label={
                        <InfoLabel info="Pseudonymous identifier for the data subject (email hash, customer ID, account number). Never store the subject's plaintext email here — this field is searchable by every admin in the workspace.">
                          Subject ID
                        </InfoLabel>
                      }
                      required
                    >
                      <Input value={subject} onChange={(_, d) => setSubject(d.value)} placeholder="user_42" />
                    </Field>
                    <Field
                      label={
                        <InfoLabel info="GDPR Article 15-20 categories. access = give the subject a copy of their data; portability = machine-readable export; rectification = fix incorrect data; erasure = delete (right to be forgotten); restriction = stop processing without deleting.">
                          Type
                        </InfoLabel>
                      }
                      required
                    >
                      <Dropdown
                        value={type}
                        selectedOptions={[type]}
                        onOptionSelect={(_, d) => setType(d.optionValue ?? "access")}
                      >
                        {TYPES.map((t) => (
                          <Option key={t} value={t}>{t}</Option>
                        ))}
                      </Dropdown>
                    </Field>
                    <Field
                      label={
                        <InfoLabel info="Internal context: how the subject contacted you, identity verification status, regulator involvement. Recorded on the request and surfaced to whoever processes it within the 30-day SLA.">
                          Notes
                        </InfoLabel>
                      }
                    >
                      <Textarea value={notes} onChange={(_, d) => setNotes(d.value)} rows={2} />
                    </Field>
                  </div>
                </DialogContent>
                <DialogActions>
                  <DialogTrigger disableButtonEnhancement>
                    <Button appearance="secondary">Cancel</Button>
                  </DialogTrigger>
                  <Button
                    appearance="primary"
                    disabled={!subject || create.isPending}
                    onClick={onCreate}
                  >
                    File request
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
            title="No DSR requests yet"
            body="Subjects can file access, portability, rectification, erasure, or restriction requests here."
          />
        )}

        {data && data.length > 0 && (
          <div className={styles.tableWrap}>
            <Table aria-label="DSR requests">
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Status</TableHeaderCell>
                  <TableHeaderCell>Type</TableHeaderCell>
                  <TableHeaderCell>Subject</TableHeaderCell>
                  <TableHeaderCell>Received</TableHeaderCell>
                  <TableHeaderCell>Due</TableHeaderCell>
                  <TableHeaderCell style={{ width: 200 }}>Advance</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageRows.map((r) => {
                  const due = new Date(r.DueAt).getTime();
                  const overdue = r.Status !== "completed" && r.Status !== "rejected" && due < now;
                  return (
                    <TableRow key={r.ID}>
                      <TableCell>
                        <Badge
                          appearance="filled"
                          color={badgeColor(r.Status) as never}
                          icon={<ShieldKeyhole20Regular />}
                        >
                          {r.Status}
                        </Badge>
                      </TableCell>
                      <TableCell style={{ width: 130 }}><span className={styles.mono}>{r.Type}</span></TableCell>
                      <TableCell style={{ maxWidth: 260 }}>
                        <Truncate text={r.SubjectID} className={styles.mono} />
                      </TableCell>
                      <TableCell>
                        <Text size={200}>{new Date(r.ReceivedAt).toLocaleString()}</Text>
                      </TableCell>
                      <TableCell>
                        <span className={overdue ? styles.dueOver : styles.due}>
                          {new Date(r.DueAt).toLocaleDateString()}
                          {overdue ? " · OVERDUE" : ""}
                        </span>
                      </TableCell>
                      <TableCell>
                        <Dropdown
                          size="small"
                          value={r.Status}
                          selectedOptions={[r.Status]}
                          onOptionSelect={(_, d) =>
                            updateStatus.mutate({
                              id: r.ID,
                              status: d.optionValue ?? r.Status,
                            })
                          }
                        >
                          {STATUS.map((s) => (
                            <Option key={s} value={s}>{s}</Option>
                          ))}
                        </Dropdown>
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
