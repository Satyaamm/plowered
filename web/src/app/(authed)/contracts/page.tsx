"use client";

import Link from "next/link";
import {
  Badge,
  Button,
  Caption1,
  Card,
  Subtitle1,
  Subtitle2,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { PlayRegular, BeakerRegular } from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  Breach,
  Contract,
  useContracts,
  useEvaluateAllContracts,
  useTenantBreaches,
} from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  card: { padding: "14px 16px" },
  contractRow: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    flexWrap: "wrap",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  spec: { display: "flex", gap: "10px", flexWrap: "wrap", marginTop: "6px" },
  breachRow: {
    display: "grid",
    gridTemplateColumns: "160px 140px 1fr",
    gap: "8px",
    padding: "6px 0",
    fontSize: "12px",
    alignItems: "center",
  },
});

export default function ContractsPage() {
  const styles = useStyles();
  const contracts = useContracts();
  const breaches = useTenantBreaches(50);
  const evalAll = useEvaluateAllContracts();

  return (
    <div className={styles.root}>
      <PageHeader
        title="Data contracts"
        subtitle="Producer-declared guarantees (schema, freshness, null fractions) the platform continuously checks against asset profiles. Breaches route through the notify dispatcher."
        crumbs={[{ label: "Home", href: "/" }, { label: "Contracts" }]}
        actions={
          <Button
            appearance="primary"
            icon={<BeakerRegular />}
            onClick={() => evalAll.mutate()}
            disabled={evalAll.isPending}
          >
            {evalAll.isPending ? "Evaluating…" : "Evaluate all"}
          </Button>
        }
      />

      {contracts.isLoading && <LoadingState />}
      {contracts.error && <ErrorBanner error={contracts.error as Error} />}
      {contracts.data && contracts.data.length === 0 && (
        <EmptyState
          title="No contracts yet"
          body="Open any asset and use the Contract panel on the Overview tab to declare expected schema, freshness, and null thresholds."
        />
      )}
      {contracts.data?.map((c) => (
        <ContractCard key={c.id} contract={c} />
      ))}

      <Card className={styles.card}>
        <Subtitle2>Recent breaches</Subtitle2>
        {breaches.isLoading && <Caption1 className={styles.meta}>Loading…</Caption1>}
        {breaches.data && breaches.data.length === 0 && (
          <Caption1 className={styles.meta}>No breaches recorded yet.</Caption1>
        )}
        {breaches.data && breaches.data.length > 0 && (
          <div style={{ marginTop: 8 }}>
            {breaches.data.map((b) => (
              <BreachRow key={b.id} breach={b} />
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function ContractCard({ contract }: { contract: Contract }) {
  const styles = useStyles();
  return (
    <Card className={styles.card}>
      <div className={styles.contractRow}>
        <Badge appearance="tint" color={statusColour(contract.status)}>
          {contract.status}
        </Badge>
        <Subtitle1>
          <Link href={`/asset/${encodeURIComponent(contract.asset_id)}`}>
            {contract.asset_id}
          </Link>
        </Subtitle1>
        <Badge appearance="tint">v{contract.version}</Badge>
        <span style={{ flex: 1 }} />
        <Caption1 className={styles.meta}>
          Updated {new Date(contract.updated_at).toLocaleString()}
        </Caption1>
      </div>
      <div className={styles.spec}>
        {!!contract.expected_columns?.length && (
          <Badge appearance="outline">
            {contract.expected_columns.length} expected col
            {contract.expected_columns.length === 1 ? "" : "s"}
          </Badge>
        )}
        {!!contract.freshness_seconds && (
          <Badge appearance="outline">
            freshness ≤ {humanise(contract.freshness_seconds)}
          </Badge>
        )}
        {contract.null_thresholds &&
          Object.keys(contract.null_thresholds).length > 0 && (
            <Badge appearance="outline">
              {Object.keys(contract.null_thresholds).length} null rule
              {Object.keys(contract.null_thresholds).length === 1 ? "" : "s"}
            </Badge>
          )}
      </div>
      {contract.description && (
        <Text style={{ fontSize: 13, marginTop: 6 }}>{contract.description}</Text>
      )}
    </Card>
  );
}

function BreachRow({ breach }: { breach: Breach }) {
  const styles = useStyles();
  return (
    <div className={styles.breachRow}>
      <Badge appearance="filled" color={severityColour(breach.severity)}>
        {breach.kind}
      </Badge>
      <span>{new Date(breach.observed_at).toLocaleString()}</span>
      <span className={styles.meta}>
        <Link href={`/asset/${encodeURIComponent(breach.asset_id)}`}>
          {breach.asset_id}
        </Link>
        {breach.message ? ` · ${breach.message}` : ""}
      </span>
    </div>
  );
}

function statusColour(s: string): "brand" | "warning" | "subtle" {
  if (s === "active") return "brand";
  if (s === "suspended") return "warning";
  return "subtle";
}

function severityColour(s: string): "danger" | "warning" | "informative" {
  if (s === "critical" || s === "error") return "danger";
  if (s === "warning") return "warning";
  return "informative";
}

function humanise(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86400)}d`;
}
