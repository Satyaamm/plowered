"use client";

import { use } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Title2,
  Subtitle2,
  Caption1,
  Body1,
  Badge,
  Spinner,
  Divider,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { api } from "@/lib/api";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "24px" },
  qn: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    color: tokens.colorNeutralForeground2,
  },
  meta: { display: "flex", gap: "12px", alignItems: "center", marginTop: "4px" },
  metaItem: {
    color: tokens.colorNeutralForeground2,
    fontSize: tokens.fontSizeBase200,
  },
  tags: { display: "flex", flexWrap: "wrap", gap: "6px" },
  lineageItem: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    color: tokens.colorNeutralForeground2,
  },
});

export default function AssetPage({ params }: { params: Promise<{ qn: string }> }) {
  const styles = useStyles();
  const { qn: encoded } = use(params);
  const qn = decodeURIComponent(encoded);

  const asset = useQuery({
    queryKey: ["asset", qn],
    queryFn: () => api.getAssetByQualifiedName(qn),
  });

  const lineage = useQuery({
    queryKey: ["lineage", asset.data?.id, "upstream"],
    queryFn: () => api.lineage(asset.data!.id, "upstream", 1),
    enabled: !!asset.data?.id,
  });

  if (asset.isLoading) return <Spinner label="Loading…" />;
  if (asset.error) return <Body1>{(asset.error as Error).message}</Body1>;
  if (!asset.data) return null;

  const a = asset.data;

  return (
    <div className={styles.root}>
      <header>
        <Caption1 className={styles.qn}>{a.qualified_name}</Caption1>
        <Title2>{a.name}</Title2>
        <div className={styles.meta}>
          <Badge appearance="outline">{a.type || "asset"}</Badge>
          <span className={styles.metaItem}>trust: {a.trust}</span>
          <span className={styles.metaItem}>
            updated {new Date(a.updated_at).toLocaleDateString()}
          </span>
        </div>
      </header>

      {a.description && (
        <section>
          <Subtitle2>Description</Subtitle2>
          <Body1>{a.description}</Body1>
        </section>
      )}

      {a.tags && a.tags.length > 0 && (
        <section>
          <Subtitle2>Tags</Subtitle2>
          <div className={styles.tags}>
            {a.tags.map((t) => (
              <Badge key={t} appearance="tint" color="brand">{t}</Badge>
            ))}
          </div>
        </section>
      )}

      <Divider />

      <section>
        <Subtitle2>Upstream lineage</Subtitle2>
        {lineage.isLoading && <Spinner size="tiny" />}
        {lineage.data && lineage.data.edges.length === 0 && <Body1>No upstream edges.</Body1>}
        {lineage.data && lineage.data.edges.length > 0 && (
          <ul style={{ paddingLeft: 0, listStyle: "none" }}>
            {lineage.data.edges.map((e) => (
              <li key={e.id} className={styles.lineageItem}>
                ← {e.source_id}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
