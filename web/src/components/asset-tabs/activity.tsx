"use client";

import { useMemo } from "react";
import {
  Badge,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { useAuditFeed } from "@/lib/hooks";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "8px" },
  row: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "10px 14px",
    display: "grid",
    gridTemplateColumns: "100px 1fr 140px",
    gap: "12px",
    alignItems: "center",
    fontSize: "13px",
  },
  method: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontWeight: 600,
    fontSize: "11px",
    color: tokens.colorBrandForeground1,
  },
  path: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
});

function outcomeColor(o?: string) {
  if (o === "denied") return "danger" as const;
  if (o === "failure") return "warning" as const;
  return "subtle" as const;
}

// ActivityTab filters the audit feed client-side for events whose
// resource_id matches this asset. The backend doesn't yet take a
// resource_id query param; once it does, switch to that for efficiency.
export function ActivityTab({ assetId }: { assetId: string }) {
  const styles = useStyles();
  const feed = useAuditFeed(500);

  const events = useMemo(() => {
    return (feed.data ?? []).filter((e: any) => e.resource_id === assetId);
  }, [feed.data, assetId]);

  if (feed.isLoading) return <LoadingState />;
  if (feed.error) return <ErrorBanner error={feed.error} />;
  if (events.length === 0) {
    return (
      <EmptyState
        title="No activity on this asset"
        body="Reads, edits, runs, and policy decisions land here as the platform sees them."
      />
    );
  }

  return (
    <div className={styles.body}>
      {events.map((e: any) => (
        <div key={e.event_id} className={styles.row}>
          <span className={styles.method}>{e.http_method ?? e.action}</span>
          <span className={styles.path}>{e.http_path ?? e.action}</span>
          <div style={{ display: "flex", gap: 8, alignItems: "center", justifyContent: "flex-end" }}>
            {e.outcome && (
              <Badge
                appearance={e.outcome === "success" ? "outline" : "filled"}
                color={outcomeColor(e.outcome)}
              >
                {e.outcome}
              </Badge>
            )}
            <span className={styles.meta}>
              {new Date(e.created_at).toLocaleString()}
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}
