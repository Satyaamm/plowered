"use client";

import Link from "next/link";
import {
  Card,
  CardHeader,
  Body1,
  Caption1,
  Badge,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import type { Asset } from "@/lib/types";

const useStyles = makeStyles({
  link: { display: "block", textDecoration: "none" },
  card: {
    "&:hover": {
      backgroundColor: tokens.colorNeutralBackground1Hover,
      cursor: "pointer",
    },
  },
  qn: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    color: tokens.colorNeutralForeground2,
    fontSize: tokens.fontSizeBase200,
  },
  body: { marginTop: "4px" },
  desc: { color: tokens.colorNeutralForeground2, marginTop: "8px" },
  tags: { display: "flex", flexWrap: "wrap", gap: "6px", marginTop: "12px" },
});

export function AssetCard({ asset }: { asset: Asset }) {
  const styles = useStyles();
  return (
    <Link href={`/asset/${encodeURIComponent(asset.qualified_name)}`} className={styles.link}>
      <Card className={styles.card}>
        <CardHeader
          header={
            <div>
              <Caption1 className={styles.qn}>{asset.qualified_name}</Caption1>
              <Body1 className={styles.body}>{asset.name}</Body1>
            </div>
          }
          action={<Badge appearance="outline">{asset.type || "asset"}</Badge>}
        />
        {asset.description && <Body1 className={styles.desc}>{asset.description}</Body1>}
        {asset.tags && asset.tags.length > 0 && (
          <div className={styles.tags}>
            {asset.tags.map((t) => (
              <Badge key={t} appearance="tint" color="brand">{t}</Badge>
            ))}
          </div>
        )}
      </Card>
    </Link>
  );
}
