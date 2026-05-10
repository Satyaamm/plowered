"use client";

import { useState } from "react";
import {
  Badge,
  Button,
  Caption1,
  Card,
  Checkbox,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Dropdown,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  Spinner,
  Subtitle2,
  Tab,
  TabList,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Add20Regular,
  Delete20Regular,
  Mail20Regular,
  Person24Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { EmptyState, ErrorBanner, LoadingState } from "@/components/states";
import {
  Invite,
  Member,
  MemberRole,
  ROLE_OPTIONS,
  useCreateInvite,
  useInvites,
  useMembers,
  useRemoveMember,
  useRevokeInvite,
  useUpdateMember,
} from "@/lib/hooks";
import { useMe } from "@/lib/auth-client";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  tabs: { marginBottom: "8px" },
  row: {
    display: "grid",
    gridTemplateColumns: "1.4fr 1fr 1fr auto",
    alignItems: "center",
    gap: "16px",
    padding: "12px 16px",
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
  },
  emailCell: { display: "flex", alignItems: "center", gap: "10px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  actions: { display: "flex", gap: "6px", justifyContent: "flex-end" },
});

export default function TeamPage() {
  const styles = useStyles();
  const [tab, setTab] = useState<"members" | "invites">("members");
  const [open, setOpen] = useState(false);
  const me = useMe();
  const members = useMembers();
  const invites = useInvites();
  const isAdmin =
    me.data?.roles?.includes("admin") || me.data?.roles?.includes("super_admin");

  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Management" }, { label: "Team" }]}
        title="Team"
        subtitle="Members and pending invitations for this workspace."
        actions={
          isAdmin && (
            <Dialog open={open} onOpenChange={(_, d) => setOpen(d.open)}>
              <DialogTrigger disableButtonEnhancement>
                <Button appearance="primary" icon={<Add20Regular />}>
                  Invite teammate
                </Button>
              </DialogTrigger>
              <InviteDialog onClose={() => setOpen(false)} />
            </Dialog>
          )
        }
      />

      <TabList
        className={styles.tabs}
        selectedValue={tab}
        onTabSelect={(_, d) => setTab(d.value as "members" | "invites")}
      >
        <Tab value="members">Members ({members.data?.length ?? 0})</Tab>
        <Tab value="invites">
          Pending invites ({invites.data?.length ?? 0})
        </Tab>
      </TabList>

      {tab === "members" && (
        <MembersList
          members={members.data}
          isLoading={members.isLoading}
          error={members.error as Error | null}
          isAdmin={!!isAdmin}
          currentUserID={me.data?.user_id}
        />
      )}
      {tab === "invites" && (
        <InvitesList
          invites={invites.data}
          isLoading={invites.isLoading}
          error={invites.error as Error | null}
          isAdmin={!!isAdmin}
        />
      )}
    </div>
  );
}

function MembersList({
  members,
  isLoading,
  error,
  isAdmin,
  currentUserID,
}: {
  members: Member[] | undefined;
  isLoading: boolean;
  error: Error | null;
  isAdmin: boolean;
  currentUserID?: string;
}) {
  const styles = useStyles();
  const update = useUpdateMember();
  const remove = useRemoveMember();

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorBanner error={error} />;
  if (!members || members.length === 0) {
    return (
      <EmptyState
        title="No members"
        body="Invite teammates to start collaborating in this workspace."
      />
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
      {members.map((m) => {
        const primaryRole = (m.roles[0] as MemberRole) ?? "viewer";
        const isSelf = m.user_id === currentUserID;
        return (
          <Card key={m.user_id} className={styles.row}>
            <div className={styles.emailCell}>
              <Person24Regular />
              <div>
                <Subtitle2>{m.full_name || m.email}</Subtitle2>
                <Caption1 className={styles.meta}>{m.email}</Caption1>
              </div>
            </div>
            <div>
              {isAdmin && !isSelf ? (
                <Dropdown
                  value={
                    ROLE_OPTIONS.find((o) => o.value === primaryRole)?.label ??
                    primaryRole
                  }
                  selectedOptions={[primaryRole]}
                  onOptionSelect={(_, d) =>
                    update.mutate({
                      userId: m.user_id,
                      roles: [d.optionValue ?? "viewer"],
                    })
                  }
                >
                  {ROLE_OPTIONS.map((r) => (
                    <Option key={r.value} value={r.value} text={r.label}>
                      {r.label}
                    </Option>
                  ))}
                </Dropdown>
              ) : (
                <Badge appearance="outline" color="brand">
                  {primaryRole}
                </Badge>
              )}
            </div>
            <Caption1 className={styles.meta}>
              {m.joined_at
                ? `Joined ${new Date(m.joined_at).toLocaleDateString()}`
                : `Invited ${new Date(m.invited_at).toLocaleDateString()}`}
            </Caption1>
            <div className={styles.actions}>
              {isAdmin && !isSelf && (
                <Button
                  appearance="subtle"
                  icon={<Delete20Regular />}
                  aria-label="Remove member"
                  onClick={() => {
                    if (
                      confirm(
                        `Remove ${m.email} from the workspace? They lose access immediately.`,
                      )
                    ) {
                      remove.mutate(m.user_id);
                    }
                  }}
                />
              )}
              {isSelf && (
                <Badge appearance="outline" color="subtle">
                  you
                </Badge>
              )}
            </div>
          </Card>
        );
      })}
    </div>
  );
}

function InvitesList({
  invites,
  isLoading,
  error,
  isAdmin,
}: {
  invites: Invite[] | undefined;
  isLoading: boolean;
  error: Error | null;
  isAdmin: boolean;
}) {
  const styles = useStyles();
  const revoke = useRevokeInvite();
  if (isLoading) return <LoadingState />;
  if (error) return <ErrorBanner error={error} />;
  if (!invites || invites.length === 0) {
    return (
      <EmptyState
        title="No pending invites"
        body={
          isAdmin
            ? "Invite a teammate from the Team page to populate this list."
            : "Once an admin sends an invite, it'll appear here."
        }
      />
    );
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "10px" }}>
      {invites.map((i) => (
        <Card key={i.id} className={styles.row}>
          <div className={styles.emailCell}>
            <Mail20Regular />
            <div>
              <Subtitle2>{i.email}</Subtitle2>
              <Caption1 className={styles.meta}>
                Expires {new Date(i.expires_at).toLocaleDateString()}
              </Caption1>
            </div>
          </div>
          <div>
            <Badge appearance="outline" color="brand">
              {i.roles.join(", ")}
            </Badge>
          </div>
          <Caption1 className={styles.meta}>{i.status}</Caption1>
          <div className={styles.actions}>
            {isAdmin && i.status === "pending" && (
              <Button
                appearance="subtle"
                icon={<Delete20Regular />}
                aria-label="Revoke invite"
                onClick={() => revoke.mutate(i.id)}
              />
            )}
          </div>
        </Card>
      ))}
    </div>
  );
}

function InviteDialog({ onClose }: { onClose: () => void }) {
  const [email, setEmail] = useState("");
  const [roles, setRoles] = useState<string[]>(["viewer"]);
  const create = useCreateInvite();

  const valid = /.+@.+\..+/.test(email) && roles.length > 0;

  const submit = async () => {
    try {
      await create.mutateAsync({ email, roles });
      onClose();
    } catch {
      // error state is on the create mutation
    }
  };

  return (
    <DialogSurface>
      <DialogBody>
        <DialogTitle>Invite teammate</DialogTitle>
        <DialogContent>
          <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
            <Field
              label="Email"
              required
              hint="They'll get an email with a 7-day-valid link to set a password and join."
            >
              <Input
                type="email"
                value={email}
                onChange={(_, d) => setEmail(d.value)}
              />
            </Field>
            <Field label="Roles" required>
              <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                {ROLE_OPTIONS.map((r) => (
                  <Checkbox
                    key={r.value}
                    label={
                      <span>
                        <strong>{r.label}</strong>{" "}
                        <Text style={{ color: tokens.colorNeutralForeground3 }}>
                          — {r.hint}
                        </Text>
                      </span>
                    }
                    checked={roles.includes(r.value)}
                    onChange={(_, d) => {
                      setRoles((prev) =>
                        d.checked
                          ? Array.from(new Set([...prev, r.value]))
                          : prev.filter((p) => p !== r.value),
                      );
                    }}
                  />
                ))}
              </div>
            </Field>
            {create.error && (
              <MessageBar intent="error">
                <MessageBarBody>
                  {(create.error as Error).message}
                </MessageBarBody>
              </MessageBar>
            )}
          </div>
        </DialogContent>
        <DialogActions>
          <DialogTrigger disableButtonEnhancement>
            <Button appearance="secondary" onClick={onClose}>
              Cancel
            </Button>
          </DialogTrigger>
          <Button
            appearance="primary"
            onClick={submit}
            disabled={!valid || create.isPending}
          >
            {create.isPending ? <Spinner size="extra-tiny" /> : "Send invite"}
          </Button>
        </DialogActions>
      </DialogBody>
    </DialogSurface>
  );
}
