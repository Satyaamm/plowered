"use client";

import Link from "next/link";
import { Text, makeStyles, tokens } from "@fluentui/react-components";

const useStyles = makeStyles({
  root: {
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    backgroundColor: tokens.colorNeutralBackground1,
  },
  inner: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    maxWidth: "1024px",
    margin: "0 auto",
    padding: "16px 24px",
  },
  brand: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    fontWeight: 700,
    fontSize: tokens.fontSizeBase400,
    color: tokens.colorBrandForeground1,
  },
  nav: {
    display: "flex",
    gap: "24px",
  },
  link: {
    color: tokens.colorNeutralForeground2,
    fontSize: tokens.fontSizeBase300,
  },
});

export function Header({ appName }: { appName: string }) {
  const styles = useStyles();
  return (
    <header className={styles.root}>
      <div className={styles.inner}>
        <Link href="/" className={styles.brand}>
          {appName}
        </Link>
        <nav className={styles.nav}>
          <Link href="/" className={styles.link}><Text>Home</Text></Link>
          <Link href="/search" className={styles.link}><Text>Search</Text></Link>
          <Link href="/pipelines" className={styles.link}><Text>Pipelines</Text></Link>
          <Link href="/runs" className={styles.link}><Text>Runs</Text></Link>
          <Link href="/checks" className={styles.link}><Text>Checks</Text></Link>
          <Link href="/alerts" className={styles.link}><Text>Alerts</Text></Link>
          <Link href="/admin/policies" className={styles.link}><Text>Policies</Text></Link>
          <Link href="/admin/audit" className={styles.link}><Text>Audit</Text></Link>
          <Link href="/admin/deleted" className={styles.link}><Text>Recycle bin</Text></Link>
        </nav>
      </div>
    </header>
  );
}
