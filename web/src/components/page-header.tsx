"use client";

import Link from "next/link";
import { Breadcrumb, BreadcrumbItem, BreadcrumbDivider, Title2, Text, makeStyles, tokens } from "@fluentui/react-components";

type Crumb = { label: string; href?: string };

const useStyles = makeStyles({
  root: {
    display: "flex",
    flexDirection: "column",
    gap: "8px",
    padding: "20px 32px 16px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    backgroundColor: tokens.colorNeutralBackground1,
    margin: "-24px -32px 24px",
  },
  crumbs: { fontSize: "12px", color: tokens.colorNeutralForeground3 },
  titleRow: {
    display: "flex",
    alignItems: "flex-end",
    justifyContent: "space-between",
    gap: "16px",
  },
  titleStack: { display: "flex", flexDirection: "column", gap: "2px" },
  subtitle: { color: tokens.colorNeutralForeground3, fontSize: "13px" },
  actions: { display: "flex", gap: "8px", alignItems: "center" },
});

export function PageHeader({
  crumbs,
  title,
  subtitle,
  actions,
}: {
  crumbs?: Crumb[];
  title: string;
  subtitle?: string;
  actions?: React.ReactNode;
}) {
  const styles = useStyles();
  return (
    <div className={styles.root}>
      {crumbs && crumbs.length > 0 && (
        <Breadcrumb size="small" className={styles.crumbs}>
          {crumbs.flatMap((c, i) => {
            const item = (
              <BreadcrumbItem key={`item-${i}`}>
                {c.href ? <Link href={c.href}>{c.label}</Link> : <Text>{c.label}</Text>}
              </BreadcrumbItem>
            );
            return i < crumbs.length - 1
              ? [item, <BreadcrumbDivider key={`d-${i}`} />]
              : [item];
          })}
        </Breadcrumb>
      )}
      <div className={styles.titleRow}>
        <div className={styles.titleStack}>
          <Title2 as="h1">{title}</Title2>
          {subtitle && <span className={styles.subtitle}>{subtitle}</span>}
        </div>
        {actions && <div className={styles.actions}>{actions}</div>}
      </div>
    </div>
  );
}
