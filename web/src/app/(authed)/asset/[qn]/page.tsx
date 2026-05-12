"use client";

import { use, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Tab,
  TabList,
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
import { QualityTab } from "@/components/asset-tabs/quality";
import { ActivityTab } from "@/components/asset-tabs/activity";

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
            <Button
              appearance="primary"
              icon={<CertificateRegular />}
              disabled
            >
              Certify (coming soon)
            </Button>
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
        {(a.tags ?? []).slice(0, 6).map((t: string) => (
          <Tooltip key={t} content={t} relationship="label">
            <Badge
              appearance="filled"
              color={
                t.startsWith("class:phi") || t.startsWith("class:pci")
                  ? "danger"
                  : t.startsWith("class:pii") || t.startsWith("class:secret")
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
        {tab === "lineage"  && <LineageTab assetId={a.id} />}
        {tab === "quality"  && <QualityTab assetId={a.id} qualifiedName={a.qualified_name} />}
        {tab === "activity" && <ActivityTab assetId={a.id} />}
      </div>
    </>
  );
}
