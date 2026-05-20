"use client";

import { use, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Field,
  Spinner,
  Tab,
  TabList,
  Textarea,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  CertificateRegular,
} from "@fluentui/react-icons";
import { api } from "@/lib/api";
import { PageHeader } from "@/components/page-header";
import { ErrorBanner, LoadingState } from "@/components/states";
import { OverviewTab } from "@/components/asset-tabs/overview";
import { SchemaTab } from "@/components/asset-tabs/schema";
import { LineageTab } from "@/components/asset-tabs/lineage";
import { ProfileTab } from "@/components/asset-tabs/profile";
import { ExploreTab } from "@/components/asset-tabs/explore";
import { QualityTab } from "@/components/asset-tabs/quality";
import { ActivityTab } from "@/components/asset-tabs/activity";
import {
  Certification,
  useAssetCertifications,
  useProposeCertification,
  useRevokeCertification,
} from "@/lib/hooks";

const useStyles = makeStyles({
  tabBar: {
    backgroundColor: tokens.colorNeutralBackground1,
    borderRadius: "6px 6px 0 0",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    padding: "4px 12px 0",
  },
  tabBody: { padding: "16px 0" },
  pillRow: { display: "flex", gap: "8px", flexWrap: "wrap", alignItems: "center" },
});

const TABS = [
  { key: "overview", label: "Overview" },
  { key: "schema",   label: "Schema",   restrictTo: ["table", "view", "schema"] as string[] | undefined },
  { key: "profile",  label: "Profile",  restrictTo: ["table", "view"] as string[] | undefined },
  { key: "explore",  label: "Explore",  restrictTo: ["table", "view"] as string[] | undefined },
  { key: "lineage",  label: "Lineage" },
  { key: "quality",  label: "Quality" },
  { key: "activity", label: "Activity" },
];

export default function AssetPage({
  params,
}: {
  params: Promise<{ qn: string }>;
}) {
  const styles = useStyles();
  const { qn: encoded } = use(params);
  const qn = decodeURIComponent(encoded);

  const asset = useQuery({
    queryKey: ["asset", qn],
    queryFn: () => api.getAssetByQualifiedName(qn),
  });

  const [tab, setTab] = useState("overview");
  const [certDialogOpen, setCertDialogOpen] = useState(false);

  if (asset.isLoading) return <LoadingState />;
  if (asset.error) return <ErrorBanner error={asset.error} />;
  if (!asset.data) return null;

  const a = asset.data as any;
  const visibleTabs = TABS.filter(
    (t) => !t.restrictTo || t.restrictTo.includes(a.type),
  );

  return (
    <>
      <PageHeader
        title={a.name}
        subtitle={a.qualified_name}
        crumbs={[
          { label: "Home", href: "/" },
          { label: "Catalog", href: "/catalog" },
          { label: a.type ?? "asset" },
        ]}
        actions={
          <>
            <Button
              icon={<ArrowSync20Regular />}
              onClick={() => asset.refetch()}
            >
              Refresh
            </Button>
            <CertificationAction
              assetId={a.id}
              open={certDialogOpen}
              setOpen={setCertDialogOpen}
            />
          </>
        }
      />

      <div className={styles.pillRow} style={{ marginBottom: 16 }}>
        <Badge appearance="tint" color="brand">{a.type}</Badge>
        <Badge
          appearance="tint"
          color={
            a.trust === "certified"
              ? "success"
              : a.trust === "deprecated"
                ? "danger"
                : "warning"
          }
        >
          trust: {a.trust ?? "unverified"}
        </Badge>
        <CertificationBadge assetId={a.id} />
        {(a.tags ?? []).slice(0, 6).map((t: string) => (
          <Tooltip key={t} content={t} relationship="label">
            <Badge
              appearance="filled"
              color={
                t.startsWith("class:secret") ||
                t.startsWith("class:phi") ||
                t.startsWith("class:pci")
                  ? "danger"
                  : t.startsWith("class:pii")
                    ? "warning"
                    : "informative"
              }
            >
              {t.replace(/^class:/, "")}
            </Badge>
          </Tooltip>
        ))}
      </div>

      <div className={styles.tabBar}>
        <TabList
          selectedValue={tab}
          onTabSelect={(_, d) => setTab(String(d.value))}
        >
          {visibleTabs.map((t) => (
            <Tab key={t.key} value={t.key}>
              {t.label}
            </Tab>
          ))}
        </TabList>
      </div>

      <div className={styles.tabBody}>
        {tab === "overview" && <OverviewTab asset={a} />}
        {tab === "schema"   && <SchemaTab assetId={a.id} />}
        {tab === "profile"  && <ProfileTab assetId={a.id} />}
        {tab === "explore"  && <ExploreTab assetId={a.id} />}
        {tab === "lineage"  && <LineageTab assetId={a.id} />}
        {tab === "quality"  && <QualityTab assetId={a.id} qualifiedName={a.qualified_name} />}
        {tab === "activity" && <ActivityTab assetId={a.id} />}
      </div>
    </>
  );
}

function CertificationBadge({ assetId }: { assetId: string }) {
  const q = useAssetCertifications(assetId);
  const c: Certification | null = q.data?.latest ?? null;
  if (!c) return null;
  const color =
    c.status === "certified" ? "success"
    : c.status === "proposed" ? "warning"
    : c.status === "rejected" ? "danger"
    : "subtle";
  return (
    <Tooltip content={c.review_note || c.justification || ""} relationship="label">
      <Badge appearance="filled" color={color}>cert: {c.status}</Badge>
    </Tooltip>
  );
}

function CertificationAction({
  assetId,
  open,
  setOpen,
}: {
  assetId: string;
  open: boolean;
  setOpen: (v: boolean) => void;
}) {
  const q = useAssetCertifications(assetId);
  const propose = useProposeCertification(assetId);
  const revoke = useRevokeCertification(assetId);
  const [note, setNote] = useState("");

  const latest = q.data?.latest;
  const isCertified = latest?.status === "certified";
  const isPending = latest?.status === "proposed";

  if (isPending) {
    return (
      <Button appearance="primary" icon={<CertificateRegular />} disabled>
        Pending review
      </Button>
    );
  }
  if (isCertified) {
    return (
      <Button
        appearance="outline"
        icon={<CertificateRegular />}
        onClick={() => {
          const reason = window.prompt("Why revoke?", "");
          if (reason !== null) revoke.mutate(reason);
        }}
        disabled={revoke.isPending}
      >
        {revoke.isPending ? "Revoking…" : "Revoke certification"}
      </Button>
    );
  }
  return (
    <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
      <Button
        appearance="primary"
        icon={<CertificateRegular />}
        onClick={() => setOpen(true)}
      >
        Propose certification
      </Button>
      <DialogSurface>
        <DialogBody>
          <DialogTitle>Propose certification</DialogTitle>
          <DialogContent>
            <Field
              label="Justification"
              hint="Why is this asset trustworthy? Stewards see this in the review queue."
            >
              <Textarea
                value={note}
                onChange={(_, d) => setNote(d.value)}
                rows={4}
              />
            </Field>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setOpen(false)}>Cancel</Button>
            <Button
              appearance="primary"
              disabled={propose.isPending}
              onClick={async () => {
                try {
                  await propose.mutateAsync(note.trim());
                  setNote("");
                  setOpen(false);
                } catch {
                  // toast surfaces the error
                }
              }}
            >
              {propose.isPending ? <Spinner size="extra-tiny" /> : "Submit"}
            </Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}
