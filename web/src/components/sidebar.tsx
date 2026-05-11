"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import {
  Caption1,
  Text,
  makeStyles,
  mergeClasses,
  tokens,
} from "@fluentui/react-components";
import {
  Home24Regular,
  Database24Regular,
  Flow24Regular,
  History24Regular,
  CheckmarkCircle24Regular,
  Shield24Regular,
  ShieldKeyhole24Regular,
  Delete24Regular,
  Gavel24Regular,
  Person24Regular,
  Settings24Regular,
  Search24Regular,
  Alert24Regular,
  Document24Regular,
  Eye24Regular,
  Sparkle24Regular,
  People24Regular,
  ChevronDoubleLeft20Regular,
  ChevronDoubleRight20Regular,
} from "@fluentui/react-icons";

type Item = {
  label: string;
  href: string;
  icon: React.ReactNode;
};

type Group = {
  heading: string;
  items: Item[];
};

const GROUPS: Group[] = [
  {
    heading: "GENERAL",
    items: [
      { label: "Home",       href: "/",        icon: <Home24Regular /> },
      { label: "Search",     href: "/search",  icon: <Search24Regular /> },
    ],
  },
  {
    heading: "CATALOG",
    items: [
      { label: "Assets",     href: "/catalog", icon: <Database24Regular /> },
    ],
  },
  {
    heading: "ORCHESTRATION",
    items: [
      { label: "Pipelines",  href: "/pipelines", icon: <Flow24Regular /> },
      { label: "Runs",       href: "/runs",      icon: <History24Regular /> },
    ],
  },
  {
    heading: "DATA QUALITY",
    items: [
      { label: "Checks",     href: "/checks", icon: <CheckmarkCircle24Regular /> },
      { label: "Alerts",     href: "/alerts", icon: <Alert24Regular /> },
    ],
  },
  {
    heading: "GOVERNANCE",
    items: [
      { label: "Policies",   href: "/admin/policies", icon: <Shield24Regular /> },
      { label: "Glossary",   href: "/glossary",       icon: <Document24Regular /> },
      { label: "Access",     href: "/access",         icon: <Eye24Regular /> },
    ],
  },
  {
    heading: "COMPLIANCE",
    items: [
      { label: "Audit log",     href: "/admin/audit",   icon: <Eye24Regular /> },
      { label: "Recycle bin",   href: "/admin/deleted", icon: <Delete24Regular /> },
      { label: "Legal holds",   href: "/legal-holds",   icon: <Gavel24Regular /> },
      { label: "DSR requests",  href: "/dsr",           icon: <ShieldKeyhole24Regular /> },
    ],
  },
  {
    heading: "MANAGEMENT",
    items: [
      { label: "Connections",  href: "/connections", icon: <Settings24Regular /> },
      { label: "Team",         href: "/team",        icon: <People24Regular /> },
      { label: "Identity",     href: "/identity",    icon: <Person24Regular /> },
      { label: "AI providers", href: "/settings/ai", icon: <Sparkle24Regular /> },
      { label: "Account",      href: "/account",     icon: <Person24Regular /> },
    ],
  },
];

const useStyles = makeStyles({
  root: {
    position: "sticky",
    top: 0,
    height: "100vh",
    display: "flex",
    flexDirection: "column",
    backgroundColor: "#FAFAFA",
    color: "#323130",
    borderRight: "1px solid #EDEBE9",
    transition: "width 160ms ease",
    overflow: "hidden",
    flexShrink: 0,
  },
  brand: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "16px 18px",
    borderBottom: "1px solid #EDEBE9",
    fontWeight: 700,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontSize: "15px",
    color: tokens.colorBrandForeground1,
    letterSpacing: "0.02em",
  },
  brandDot: {
    width: "10px",
    height: "10px",
    borderRadius: "2px",
    backgroundColor: tokens.colorBrandBackground,
  },
  scroll: { flex: 1, overflowY: "auto", padding: "8px 0" },
  groupHead: {
    padding: "12px 18px 6px",
    color: "#605E5C",
    letterSpacing: "0.08em",
    fontSize: "10px",
    fontWeight: 700,
    textTransform: "uppercase",
  },
  link: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "8px 16px",
    margin: "1px 8px",
    borderRadius: "4px",
    color: "#323130",
    textDecoration: "none",
    fontSize: "13px",
    transition: "background-color 80ms",
    ":hover": { backgroundColor: "#F3F2F1" },
  },
  linkActive: {
    backgroundColor: tokens.colorBrandBackground2,
    color: tokens.colorBrandForeground1,
    borderLeft: `3px solid ${tokens.colorBrandBackground}`,
    paddingLeft: "13px",
  },
  iconSlot: {
    width: "20px", height: "20px",
    display: "flex", alignItems: "center", justifyContent: "center",
    color: "#605E5C",
  },
  iconActive: { color: tokens.colorBrandForeground1 },
  collapseBtn: {
    margin: "8px 12px",
    padding: "8px",
    border: "none",
    background: "transparent",
    color: "#605E5C",
    cursor: "pointer",
    borderRadius: "4px",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    ":hover": { backgroundColor: "#F3F2F1", color: "#323130" },
  },
  collapsedLabel: { display: "none" },
  collapsedHead: { display: "none" },
});

export function Sidebar({ appName }: { appName: string }) {
  const styles = useStyles();
  const path = usePathname() ?? "/";
  const [collapsed, setCollapsed] = useState(false);

  return (
    <aside
      className={styles.root}
      style={{ width: collapsed ? 64 : 240 }}
      aria-label="Primary navigation"
    >
      <div className={styles.brand}>
        <span className={styles.brandDot} />
        {!collapsed && <span>{appName}</span>}
      </div>

      <div className={styles.scroll}>
        {GROUPS.map((g) => (
          <div key={g.heading}>
            {!collapsed && (
              <Caption1 className={styles.groupHead} block>
                {g.heading}
              </Caption1>
            )}
            {g.items.map((it) => {
              const active =
                it.href === "/" ? path === "/" : path.startsWith(it.href);
              return (
                <Link
                  key={it.href}
                  href={it.href}
                  className={mergeClasses(styles.link, active && styles.linkActive)}
                  title={collapsed ? it.label : undefined}
                >
                  <span
                    className={mergeClasses(
                      styles.iconSlot,
                      active && styles.iconActive,
                    )}
                  >
                    {it.icon}
                  </span>
                  {!collapsed && <Text>{it.label}</Text>}
                </Link>
              );
            })}
          </div>
        ))}
      </div>

      <button
        type="button"
        className={styles.collapseBtn}
        onClick={() => setCollapsed((c) => !c)}
        aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
      >
        {collapsed ? <ChevronDoubleRight20Regular /> : <ChevronDoubleLeft20Regular />}
      </button>
    </aside>
  );
}
