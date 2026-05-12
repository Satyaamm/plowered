"use client";

import { useRouter } from "next/navigation";
import { useState, type KeyboardEvent } from "react";
import {
  Avatar,
  Badge,
  Input,
  Menu,
  MenuDivider,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Tooltip,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Search20Regular,
  Building20Regular,
  Person20Regular,
  People20Regular,
  PlugConnected20Regular,
  Sparkle20Regular,
  BookOpen20Regular,
  ChevronDown16Regular,
  CheckmarkCircle16Filled,
  SignOut20Regular,
} from "@fluentui/react-icons";
import { usePrincipal } from "@/lib/hooks";
import { useLogout, useMyWorkspaces } from "@/lib/auth-client";

const useStyles = makeStyles({
  root: {
    height: "52px",
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "0 16px",
    backgroundColor: tokens.colorNeutralBackground1,
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    position: "sticky",
    top: 0,
    zIndex: 10,
  },
  left: { display: "flex", alignItems: "center", gap: "12px", flex: 1, minWidth: 0 },
  right: { display: "flex", alignItems: "center", gap: "6px" },
  search: {
    width: "min(520px, 50vw)",
    backgroundColor: tokens.colorNeutralBackground2,
  },
  // Workspace switcher — replaces the raw-UUID chip. Looks like a
  // pill-shaped button: building icon, workspace name, env tag,
  // chevron. Matches Vercel / Linear / GitHub patterns.
  wsButton: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    padding: "4px 10px 4px 8px",
    borderRadius: "6px",
    cursor: "pointer",
    backgroundColor: "transparent",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    color: tokens.colorNeutralForeground1,
    fontSize: "13px",
    fontWeight: 600,
    maxWidth: "260px",
    ":hover": { backgroundColor: tokens.colorNeutralBackground2 },
  },
  wsName: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  wsHead: {
    padding: "10px 12px",
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  wsHeadLabel: {
    fontSize: "10px",
    color: tokens.colorNeutralForeground3,
    fontWeight: 600,
    letterSpacing: "0.06em",
    textTransform: "uppercase",
  },
  wsHeadEmail: { fontSize: "12px", color: tokens.colorNeutralForeground3 },
  identity: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "4px 10px 4px 6px",
    borderRadius: "6px",
    cursor: "pointer",
    ":hover": { backgroundColor: tokens.colorNeutralBackground2 },
    border: "none",
    backgroundColor: "transparent",
  },
  identityName: { fontSize: "13px", fontWeight: 600, color: tokens.colorNeutralForeground1, textAlign: "left" },
  identitySub: { fontSize: "11px", color: tokens.colorNeutralForeground3, textAlign: "left" },
  userHead: {
    padding: "12px 14px",
    display: "flex",
    flexDirection: "column",
    gap: "2px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    minWidth: "240px",
  },
  userHeadName: { fontSize: "13px", fontWeight: 600 },
  userHeadEmail: { fontSize: "12px", color: tokens.colorNeutralForeground3 },
  userHeadRoles: {
    fontSize: "11px",
    color: tokens.colorBrandForeground1,
    fontWeight: 600,
    marginTop: "2px",
  },
  envBadge: { fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace" },
});

export function Topbar() {
  const styles = useStyles();
  const router = useRouter();
  const { principal } = usePrincipal();
  const workspaces = useMyWorkspaces();
  const logout = useLogout();
  const env = process.env.NEXT_PUBLIC_PLOWERED_ENV ?? "dev";
  const [query, setQuery] = useState("");

  const onSignOut = async () => {
    await logout.mutateAsync();
    router.replace("/login");
  };

  const onSearchKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && query.trim()) {
      router.push(`/search?q=${encodeURIComponent(query.trim())}`);
    }
  };

  const initials = ((principal?.fullName || principal?.email) ?? "u")
    .split(/[\s@.]+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((s) => s[0]?.toUpperCase() ?? "")
    .join("") || "U";

  const wsName = principal?.tenantName || principal?.tenantSlug || "Workspace";

  return (
    <header className={styles.root}>
      <div className={styles.left}>
        <Input
          className={styles.search}
          contentBefore={<Search20Regular />}
          placeholder="Search assets, pipelines, runs, audit events…  (press Enter)"
          appearance="filled-darker"
          size="medium"
          aria-label="Global search"
          value={query}
          onChange={(_, d) => setQuery(d.value)}
          onKeyDown={onSearchKey}
        />
      </div>

      <div className={styles.right}>
        {/* Workspace switcher */}
        <Menu>
          <MenuTrigger disableButtonEnhancement>
            <button
              type="button"
              className={styles.wsButton}
              aria-label="Switch workspace"
              title="Switch workspace"
            >
              <Building20Regular />
              <span className={styles.wsName}>{wsName}</span>
              <ChevronDown16Regular />
            </button>
          </MenuTrigger>
          <MenuPopover>
            <div className={styles.wsHead}>
              <span className={styles.wsHeadLabel}>Workspaces</span>
              <span className={styles.wsHeadEmail}>{principal?.email}</span>
            </div>
            <MenuList>
              {(workspaces.data ?? []).map((ws) => {
                const active = ws.id === principal?.tenantId;
                return (
                  <MenuItem
                    key={ws.id}
                    icon={
                      active ? (
                        <CheckmarkCircle16Filled
                          style={{ color: tokens.colorBrandForeground1 }}
                        />
                      ) : (
                        <Building20Regular />
                      )
                    }
                    onClick={() => {
                      // Single-session-per-tenant. Switching across
                      // workspaces requires a new login; surface that
                      // honestly instead of silently failing.
                      if (!active) {
                        router.push(`/login?workspace=${ws.slug}`);
                      }
                    }}
                  >
                    <span style={{ fontWeight: active ? 600 : 400 }}>
                      {ws.name}
                    </span>
                    <span
                      style={{
                        marginLeft: 8,
                        color: tokens.colorNeutralForeground3,
                        fontSize: 11,
                      }}
                    >
                      {ws.slug}
                    </span>
                  </MenuItem>
                );
              })}
              <MenuDivider />
              <MenuItem onClick={() => router.push("/team")}>
                Manage workspace…
              </MenuItem>
            </MenuList>
          </MenuPopover>
        </Menu>

        {/* Environment badge — yellow in non-prod, red in prod. Read-only. */}
        <Tooltip
          content={
            env === "production"
              ? "Production environment"
              : `${env} environment (non-production)`
          }
          relationship="label"
        >
          <Badge
            appearance="tint"
            color={env === "production" ? "danger" : "warning"}
            className={styles.envBadge}
            size="small"
          >
            {env}
          </Badge>
        </Tooltip>

        {/* User identity + dropdown */}
        <Menu>
          <MenuTrigger disableButtonEnhancement>
            <button type="button" className={styles.identity} aria-label="Account menu">
              <Avatar
                color="brand"
                initials={initials}
                size={28}
                name={principal?.fullName || principal?.email}
              />
              <span style={{ display: "flex", flexDirection: "column" }}>
                <span className={styles.identityName}>
                  {principal?.fullName || principal?.email || "Signed in"}
                </span>
                <span className={styles.identitySub}>
                  {(principal?.roles ?? []).join(" · ") || "—"}
                </span>
              </span>
            </button>
          </MenuTrigger>
          <MenuPopover>
            <div className={styles.userHead}>
              <span className={styles.userHeadName}>
                {principal?.fullName || "Signed in"}
              </span>
              <span className={styles.userHeadEmail}>{principal?.email}</span>
              {(principal?.roles ?? []).length > 0 && (
                <span className={styles.userHeadRoles}>
                  {(principal?.roles ?? []).join(" · ")}
                </span>
              )}
            </div>
            <MenuList>
              <MenuItem
                icon={<Person20Regular />}
                onClick={() => router.push("/account")}
              >
                Account settings
              </MenuItem>
              <MenuDivider />
              <MenuItem
                icon={<People20Regular />}
                onClick={() => router.push("/team")}
              >
                Team &amp; invites
              </MenuItem>
              <MenuItem
                icon={<PlugConnected20Regular />}
                onClick={() => router.push("/connections")}
              >
                Connections
              </MenuItem>
              <MenuItem
                icon={<Sparkle20Regular />}
                onClick={() => router.push("/settings/ai")}
              >
                AI providers
              </MenuItem>
              <MenuDivider />
              <MenuItem
                icon={<BookOpen20Regular />}
                onClick={() => window.open("/docs", "_blank")}
              >
                API docs
              </MenuItem>
              <MenuDivider />
              <MenuItem
                icon={<SignOut20Regular />}
                onClick={onSignOut}
                disabled={logout.isPending}
              >
                Sign out
              </MenuItem>
            </MenuList>
          </MenuPopover>
        </Menu>
      </div>
    </header>
  );
}
