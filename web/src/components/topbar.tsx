"use client";

import { useRouter } from "next/navigation";
import {
  Avatar,
  Badge,
  Input,
  Menu,
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
  Alert20Regular,
  Settings20Regular,
  QuestionCircle20Regular,
  Building20Regular,
  Person20Regular,
  SignOut20Regular,
} from "@fluentui/react-icons";
import { usePrincipal } from "@/lib/hooks";
import { useLogout } from "@/lib/auth-client";

const useStyles = makeStyles({
  root: {
    height: "48px",
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    padding: "0 16px",
    backgroundColor: "#FAF6F0",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    position: "sticky",
    top: 0,
    zIndex: 10,
  },
  left: { display: "flex", alignItems: "center", gap: "12px", flex: 1 },
  right: { display: "flex", alignItems: "center", gap: "8px" },
  search: {
    width: "min(520px, 50vw)",
    backgroundColor: tokens.colorNeutralBackground2,
  },
  envChip: {
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
  },
  iconBtn: {
    width: "36px",
    height: "36px",
    border: "none",
    background: "transparent",
    borderRadius: "4px",
    cursor: "pointer",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    color: tokens.colorNeutralForeground2,
    ":hover": { backgroundColor: tokens.colorNeutralBackground2 },
  },
  divider: {
    height: "20px",
    width: "1px",
    backgroundColor: tokens.colorNeutralStroke2,
    margin: "0 4px",
  },
  identity: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    padding: "4px 10px 4px 6px",
    borderRadius: "4px",
    cursor: "pointer",
    ":hover": { backgroundColor: tokens.colorNeutralBackground2 },
    border: "none",
    backgroundColor: "transparent",
  },
  identityName: { fontSize: "13px", fontWeight: 600, color: tokens.colorNeutralForeground1, textAlign: "left" },
  identitySub: { fontSize: "11px", color: tokens.colorNeutralForeground3, textAlign: "left" },
});

export function Topbar() {
  const styles = useStyles();
  const router = useRouter();
  const { principal } = usePrincipal();
  const logout = useLogout();
  const env = process.env.NEXT_PUBLIC_PLOWERED_ENV ?? "dev";

  const onSignOut = async () => {
    await logout.mutateAsync();
    router.replace("/login");
  };

  const initials = (principal?.email ?? "u")
    .slice(0, 2)
    .toUpperCase();

  return (
    <header className={styles.root}>
      <div className={styles.left}>
        <Input
          className={styles.search}
          contentBefore={<Search20Regular />}
          placeholder="Search assets, pipelines, runs, audit events…"
          appearance="filled-darker"
          size="medium"
          aria-label="Global search"
        />
      </div>

      <div className={styles.right}>
        <Tooltip content="Tenant" relationship="label">
          <Badge
            appearance="outline"
            color="brand"
            icon={<Building20Regular />}
            className={styles.envChip}
          >
            {(principal?.tenantId ?? "—").slice(0, 8)}
          </Badge>
        </Tooltip>
        <Tooltip content="Environment" relationship="label">
          <Badge
            appearance="tint"
            color={env === "production" ? "danger" : "warning"}
            className={styles.envChip}
          >
            {env}
          </Badge>
        </Tooltip>
        <span className={styles.divider} />
        <Tooltip content="Notifications" relationship="label">
          <button type="button" className={styles.iconBtn} aria-label="Notifications">
            <Alert20Regular />
          </button>
        </Tooltip>
        <Tooltip content="Help" relationship="label">
          <button type="button" className={styles.iconBtn} aria-label="Help">
            <QuestionCircle20Regular />
          </button>
        </Tooltip>
        <Tooltip content="Settings" relationship="label">
          <button type="button" className={styles.iconBtn} aria-label="Settings">
            <Settings20Regular />
          </button>
        </Tooltip>
        <span className={styles.divider} />

        <Menu>
          <MenuTrigger disableButtonEnhancement>
            <button type="button" className={styles.identity} aria-label="Account">
              <Avatar
                color="brand"
                initials={initials}
                size={28}
                name={principal?.email}
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
            <MenuList>
              <MenuItem icon={<Person20Regular />} onClick={() => router.push("/identity")}>
                Account
              </MenuItem>
              <MenuItem icon={<Settings20Regular />} onClick={() => router.push("/connections")}>
                Workspace settings
              </MenuItem>
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
