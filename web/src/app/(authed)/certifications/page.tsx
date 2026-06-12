"use client";

import { useState } from "react";
import Link from "next/link";
import {
  Badge,
  Button,
  Card,
  Caption1,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Field,
  Spinner,
  Subtitle1,
  Textarea,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { CheckmarkRegular, DismissRegular } from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { PageIntro } from "@/components/page-intro";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  Certification,
  useApproveCertification,
  usePendingCertifications,
  useRejectCertification,
  useRole,
} from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "12px" },
  row: {
    padding: "12px 16px",
    display: "flex",
    alignItems: "center",
    gap: "12px",
    flexWrap: "wrap",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  body: { flex: 1, minWidth: "260px", display: "flex", flexDirection: "column", gap: "4px" },
  actions: { display: "flex", gap: "8px" },
});

export default function CertificationsQueuePage() {
  const styles = useStyles();
  const q = usePendingCertifications();

  return (
    <div className={styles.root}>
      <PageHeader
        title="Certification review queue"
        subtitle="Approve or reject pending certification proposals. Approved assets surface a certified badge across the catalog."
        crumbs={[{ label: "Home", href: "/" }, { label: "Certifications" }]}
        actions={
          <PageIntro
            title="What are certifications?"
            body="A certification is a human trust badge on an asset: a steward says 'this is the canonical table for X, use this one.' An analyst searching for 'users' might find fifty matching tables — the certified one bubbles to the top with a badge so they don't have to guess."
            bullets={[
              "Stewards propose certification; admins approve or reject here in the queue; anyone with the certify role can revoke later.",
              "Use it for the one official table per domain: the gold customer mart, the canonical orders fact, the blessed feature table.",
              "Different from contracts: a contract enforces shape and freshness automatically; a certification is a human stamp that this asset is the one to trust.",
            ]}
            cta="Get started: open any asset → Overview tab → Propose certification. Approvers see the request here."
          />
        }
      />

      {q.isLoading && <LoadingState />}
      {q.error && <ErrorBanner error={q.error as Error} />}
      {q.data && q.data.length === 0 && (
        <EmptyState
          title="No pending proposals"
          body="When a user proposes an asset for certification it appears here for review."
        />
      )}
      {q.data?.map((c) => (
        <ProposalRow key={c.id} cert={c} />
      ))}
    </div>
  );
}

function ProposalRow({ cert }: { cert: Certification }) {
  const styles = useStyles();
  const approve = useApproveCertification();
  const reject = useRejectCertification();
  const { can } = useRole();
  const canReview = can("certify");
  const [open, setOpen] = useState<"approve" | "reject" | null>(null);
  const [note, setNote] = useState("");

  const close = () => {
    setOpen(null);
    setNote("");
  };

  return (
    <Card className={styles.row}>
      <Badge appearance="filled" color="warning">proposed</Badge>
      <div className={styles.body}>
        <Subtitle1>
          <Link href={`/asset/${encodeURIComponent(cert.asset_id)}`}>
            {cert.asset_id}
          </Link>
        </Subtitle1>
        <Caption1 className={styles.meta}>
          Proposed {new Date(cert.proposed_at).toLocaleString()}
          {cert.proposed_by ? ` by ${cert.proposed_by}` : ""}
        </Caption1>
        {cert.justification && (
          <span style={{ fontSize: 13 }}>{cert.justification}</span>
        )}
      </div>
      {canReview ? (
        <div className={styles.actions}>
          <Button
            appearance="primary"
            icon={<CheckmarkRegular />}
            onClick={() => setOpen("approve")}
          >
            Approve
          </Button>
          <Button
            appearance="subtle"
            icon={<DismissRegular />}
            onClick={() => setOpen("reject")}
          >
            Reject
          </Button>
        </div>
      ) : (
        <Caption1 className={styles.meta}>
          Approval requires the steward or admin role.
        </Caption1>
      )}
      <Dialog open={open !== null} onOpenChange={(_, d) => !d.open && close()}>
        <DialogSurface>
          <DialogBody>
            <DialogTitle>
              {open === "approve" ? "Approve certification" : "Reject certification"}
            </DialogTitle>
            <DialogContent>
              <Field
                label="Review note"
                hint="Optional context. Saved to the audit history."
              >
                <Textarea
                  value={note}
                  onChange={(_, d) => setNote(d.value)}
                  rows={4}
                />
              </Field>
            </DialogContent>
            <DialogActions>
              <Button onClick={close}>Cancel</Button>
              <Button
                appearance="primary"
                disabled={approve.isPending || reject.isPending}
                onClick={async () => {
                  try {
                    if (open === "approve") {
                      await approve.mutateAsync({ id: cert.id, note });
                    } else {
                      await reject.mutateAsync({ id: cert.id, note });
                    }
                    close();
                  } catch {
                    // toast surfaces error
                  }
                }}
              >
                {approve.isPending || reject.isPending ? (
                  <Spinner size="extra-tiny" />
                ) : open === "approve" ? (
                  "Approve"
                ) : (
                  "Reject"
                )}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </Card>
  );
}
