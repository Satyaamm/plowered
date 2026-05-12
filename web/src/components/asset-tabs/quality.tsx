"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge,
  Body1,
  Button,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Add20Regular } from "@fluentui/react-icons";
import Link from "next/link";
import { checksApi } from "@/lib/api";
import { CheckDesigner } from "@/components/check-designer";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import type { Check } from "@/lib/types-orchestration";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "12px" },
  row: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "12px 16px",
    display: "grid",
    gridTemplateColumns: "1fr auto",
    gap: "12px",
    alignItems: "center",
  },
  name: { fontWeight: 600 },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
});

function outcomeBadge(o?: string) {
  switch (o) {
    case "pass":  return <Badge appearance="filled" color="success">passing</Badge>;
    case "fail":  return <Badge appearance="filled" color="danger">failing</Badge>;
    case "error": return <Badge appearance="filled" color="warning">error</Badge>;
    default:      return <Badge appearance="tint" color="subtle">no runs yet</Badge>;
  }
}

export function QualityTab({ assetId, qualifiedName }: { assetId: string; qualifiedName?: string }) {
  const styles = useStyles();
  const [open, setOpen] = useState(false);

  const checks = useQuery({
    queryKey: ["checks", "byAsset", assetId],
    queryFn: () => checksApi.list({ assetId }),
  });

  if (checks.isLoading) return <LoadingState />;
  if (checks.error) return <ErrorBanner error={checks.error} />;

  const list: Check[] = (checks.data?.checks ?? []) as Check[];
  const headerActions = (
    <div style={{ display: "flex", justifyContent: "flex-end" }}>
      <Button appearance="primary" icon={<Add20Regular />} onClick={() => setOpen(true)}>
        Author check
      </Button>
    </div>
  );

  return (
    <div className={styles.body}>
      {headerActions}
      {list.length === 0 ? (
        <EmptyState
          title="No checks on this asset"
          body="Author a row-count, freshness, not-null, uniqueness, or custom-SQL check."
        />
      ) : (
        list.map((c) => (
          <div key={c.ID} className={styles.row}>
            <div>
              <Link
                className={styles.name}
                href={`/checks/${encodeURIComponent(c.ID)}`}
                style={{ color: tokens.colorBrandForeground1, textDecoration: "none" }}
              >
                {c.Name}
              </Link>
              <Body1 className={styles.meta}>
                {c.Type} · severity {c.Severity ?? "info"} · {c.Enabled ? "enabled" : "disabled"}
              </Body1>
            </div>
            <div>{outcomeBadge((c as Check & { last_outcome?: string }).last_outcome)}</div>
          </div>
        ))
      )}

      <CheckDesigner
        open={open}
        onClose={() => setOpen(false)}
        fixedAsset={{ id: assetId, qn: qualifiedName }}
      />
    </div>
  );
}
