"use client";

import Link from "next/link";
import {
  Subtitle2,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Database24Filled,
  Flow24Filled,
  History24Filled,
  CheckmarkCircle24Filled,
  Alert24Filled,
  Shield24Filled,
  Document24Filled,
  Eye24Filled,
  Delete24Filled,
  Gavel24Filled,
  ShieldKeyhole24Filled,
  Settings24Filled,
  Person24Filled,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";

const SECTIONS = [
  {
    heading: "Catalog",
    items: [
      { name: "Assets", desc: "Browse the catalog graph", href: "/catalog", icon: <Database24Filled /> },
    ],
  },
  {
    heading: "Orchestration",
    items: [
      { name: "Pipelines", desc: "Create and trigger jobs", href: "/pipelines", icon: <Flow24Filled /> },
      { name: "Runs", desc: "Pipeline + task execution history", href: "/runs", icon: <History24Filled /> },
    ],
  },
  {
    heading: "Data quality",
    items: [
      { name: "Checks", desc: "Assertions on assets", href: "/checks", icon: <CheckmarkCircle24Filled /> },
      { name: "Alerts", desc: "Notification rules + delivery", href: "/alerts", icon: <Alert24Filled /> },
    ],
  },
  {
    heading: "Governance",
    items: [
      { name: "Policies", desc: "ABAC rules, deny-overrides", href: "/admin/policies", icon: <Shield24Filled /> },
      { name: "Glossary", desc: "Business definitions", href: "/glossary", icon: <Document24Filled /> },
    ],
  },
  {
    heading: "Compliance",
    items: [
      { name: "Audit log", desc: "Hash-chained, append-only", href: "/admin/audit", icon: <Eye24Filled /> },
      { name: "Recycle bin", desc: "Restorable tombstones", href: "/admin/deleted", icon: <Delete24Filled /> },
      { name: "Legal holds", desc: "Block delete during litigation", href: "/legal-holds", icon: <Gavel24Filled /> },
      { name: "DSR requests", desc: "GDPR Art. 15-20", href: "/dsr", icon: <ShieldKeyhole24Filled /> },
    ],
  },
  {
    heading: "Management",
    items: [
      { name: "Connections", desc: "Datasources + secrets", href: "/connections", icon: <Settings24Filled /> },
      { name: "Identity", desc: "Users, sessions, API keys", href: "/identity", icon: <Person24Filled /> },
    ],
  },
];

const useStyles = makeStyles({
  body: { display: "flex", flexDirection: "column", gap: "28px" },
  group: { display: "flex", flexDirection: "column", gap: "12px" },
  grid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
    gap: "12px",
  },
  card: {
    display: "flex",
    gap: "12px",
    padding: "14px",
    borderRadius: "6px",
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    backgroundColor: tokens.colorNeutralBackground1,
    textDecoration: "none",
    color: "inherit",
    ":hover": {
      boxShadow: `0 0 0 1px ${tokens.colorBrandStroke2}`,
      backgroundColor: tokens.colorNeutralBackground2,
    },
  },
  icon: {
    width: "36px",
    height: "36px",
    borderRadius: "4px",
    backgroundColor: "#FBF1EB",
    color: "#B8521B",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
  },
  name: { fontSize: "14px", fontWeight: 600 },
  desc: { fontSize: "12px", color: tokens.colorNeutralForeground3 },
});

export default function AllServices() {
  const styles = useStyles();
  return (
    <>
      <PageHeader
        title="All services"
        subtitle="Every capability the platform exposes, grouped by domain."
        crumbs={[{ label: "Home", href: "/" }, { label: "All services" }]}
      />
      <div className={styles.body}>
        {SECTIONS.map((s) => (
          <div key={s.heading} className={styles.group}>
            <Subtitle2>{s.heading}</Subtitle2>
            <div className={styles.grid}>
              {s.items.map((it) => (
                <Link key={it.href} href={it.href} className={styles.card}>
                  <span className={styles.icon}>{it.icon}</span>
                  <div>
                    <div className={styles.name}>{it.name}</div>
                    <div className={styles.desc}>{it.desc}</div>
                  </div>
                </Link>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
