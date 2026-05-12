"use client";

import Link from "next/link";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbDivider,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Home16Regular } from "@fluentui/react-icons";

type Crumb = { label: string; href?: string };

const useStyles = makeStyles({
  root: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "16px",
    padding: "12px 32px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    backgroundColor: tokens.colorNeutralBackground1,
    margin: "-24px -32px 20px",
  },
  crumbs: { fontSize: "13px", color: tokens.colorNeutralForeground2 },
  homeLink: {
    display: "inline-flex",
    alignItems: "center",
    gap: "4px",
    color: tokens.colorNeutralForeground2,
    textDecoration: "none",
    ":hover": { color: tokens.colorBrandForeground1 },
  },
  actions: { display: "flex", gap: "8px", alignItems: "center" },
});

/**
 * Minimal page header — breadcrumb + action buttons only.
 *
 * Page titles are intentionally NOT rendered here. The sidebar already
 * tells the user where they are; repeating it as an H1 with a marketing
 * subtitle is noise on a daily-use B2B surface. If a page needs a real
 * title (e.g. an asset name on a detail page), it should render that
 * inline in its own content area, not here.
 *
 * `title` and `subtitle` remain in the prop type for back-compat with
 * existing call sites; they are ignored.
 */
export function PageHeader({
  crumbs,
  actions,
}: {
  crumbs?: Crumb[];
  /** @deprecated rendered nowhere — drop from call sites when convenient */
  title?: string;
  /** @deprecated rendered nowhere */
  subtitle?: string;
  actions?: React.ReactNode;
}) {
  const styles = useStyles();
  // First crumb is always "Home" with a home icon, even if the caller
  // forgot to pass it. After Home, render only crumbs that aren't a
  // duplicate of it.
  const tail: Crumb[] = (crumbs ?? []).filter(
    (c) => (c.label || "").toLowerCase() !== "home",
  );

  return (
    <div className={styles.root}>
      <Breadcrumb size="medium" className={styles.crumbs}>
        <BreadcrumbItem>
          <Link href="/" className={styles.homeLink}>
            <Home16Regular />
            <span>Home</span>
          </Link>
        </BreadcrumbItem>
        {tail.flatMap((c, i) => [
          <BreadcrumbDivider key={`d-${i}`} />,
          <BreadcrumbItem key={`item-${i}`}>
            {c.href ? (
              <Link
                href={c.href}
                style={{ color: "inherit", textDecoration: "none" }}
              >
                {c.label}
              </Link>
            ) : (
              <Text style={{ fontWeight: i === tail.length - 1 ? 600 : 400 }}>
                {c.label}
              </Text>
            )}
          </BreadcrumbItem>,
        ])}
      </Breadcrumb>
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  );
}
