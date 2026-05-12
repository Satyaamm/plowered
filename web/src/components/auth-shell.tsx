"use client";

import { ReactNode } from "react";
import {
  Body1,
  Caption1,
  Title2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";

const useStyles = makeStyles({
  page: {
    minHeight: "100vh",
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    justifyContent: "center",
    padding: "32px 16px",
    backgroundColor: "#F5F5F5",
  },
  brand: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    marginBottom: "20px",
    color: tokens.colorBrandForeground1,
    fontWeight: 700,
    fontSize: "16px",
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    letterSpacing: "0.02em",
  },
  brandDot: {
    width: "12px",
    height: "12px",
    borderRadius: "2px",
    backgroundColor: tokens.colorBrandBackground,
  },
  card: {
    width: "100%",
    maxWidth: "420px",
    backgroundColor: "#ffffff",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: "8px",
    boxShadow: "0 4px 24px rgba(38,28,16,0.06)",
    padding: "32px",
    display: "flex",
    flexDirection: "column",
    gap: "20px",
  },
  head: { display: "flex", flexDirection: "column", gap: "6px" },
  subtitle: { color: tokens.colorNeutralForeground3 },
  footer: {
    marginTop: "20px",
    display: "flex",
    justifyContent: "center",
    color: tokens.colorNeutralForeground3,
    fontSize: "11px",
  },
});

export function AuthShell({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
}) {
  const styles = useStyles();
  return (
    <div className={styles.page}>
      <div className={styles.brand}>
        <span className={styles.brandDot} />
        <span>PurpleCube AI Studio</span>
      </div>
      <div className={styles.card}>
        <div className={styles.head}>
          <Title2 as="h1">{title}</Title2>
          {subtitle && <Body1 className={styles.subtitle}>{subtitle}</Body1>}
        </div>
        {children}
      </div>
      <Caption1 className={styles.footer}>
        © PurpleCube AI Studio · data context platform
      </Caption1>
    </div>
  );
}
