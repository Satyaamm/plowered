"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import {
  Badge,
  Body1,
  Button,
  Caption1,
  Card,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Spinner,
  Subtitle2,
  Tab,
  TabList,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  Delete20Regular,
  Person24Regular,
  ShieldKeyhole24Regular,
  Phone24Regular,
} from "@fluentui/react-icons";
import { PageHeader } from "@/components/page-header";
import { LoadingState } from "@/components/states";
import { InfoLabel } from "@/components/info-label";
import { useMe, useLogout } from "@/lib/auth-client";
import {
  useAccountSessions,
  useChangePassword,
  useRevokeSession,
  useSignOutEverywhere,
  useUpdateProfile,
} from "@/lib/hooks";

const useStyles = makeStyles({
  root: { display: "flex", flexDirection: "column", gap: "16px" },
  card: {
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
    padding: "20px",
    display: "flex",
    flexDirection: "column",
    gap: "12px",
  },
  row: { display: "grid", gridTemplateColumns: "1fr 1fr", gap: "12px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
  sessionRow: {
    display: "grid",
    gridTemplateColumns: "1fr auto",
    gap: "12px",
    alignItems: "center",
    padding: "12px 14px",
    backgroundColor: tokens.colorNeutralBackground1,
    boxShadow: `0 0 0 1px ${tokens.colorNeutralStroke2}`,
    borderRadius: "6px",
  },
});

export default function AccountPage() {
  const styles = useStyles();
  const me = useMe();
  const [tab, setTab] = useState<"profile" | "security" | "sessions">("profile");

  if (me.isLoading) return <LoadingState />;
  if (!me.data) {
    return (
      <Body1>You must be signed in to view your account settings.</Body1>
    );
  }
  return (
    <div className={styles.root}>
      <PageHeader
        crumbs={[{ label: "Management" }, { label: "Account" }]}
        title="Account settings"
        subtitle={`Signed in as ${me.data.email}`}
      />
      <TabList
        selectedValue={tab}
        onTabSelect={(_, d) => setTab(d.value as typeof tab)}
      >
        <Tab value="profile" icon={<Person24Regular />}>
          Profile
        </Tab>
        <Tab value="security" icon={<ShieldKeyhole24Regular />}>
          Password
        </Tab>
        <Tab value="sessions" icon={<Phone24Regular />}>
          Active sessions
        </Tab>
      </TabList>
      {tab === "profile" && <ProfileTab me={me.data} />}
      {tab === "security" && <SecurityTab />}
      {tab === "sessions" && <SessionsTab />}
    </div>
  );
}

function ProfileTab({ me }: { me: { full_name: string; email: string } }) {
  const styles = useStyles();
  const update = useUpdateProfile();
  const [firstName, setFirstName] = useState(me.full_name.split(" ")[0] ?? "");
  const [lastName, setLastName] = useState(
    me.full_name.split(" ").slice(1).join(" ") ?? "",
  );
  const [phoneCountry, setPhoneCountry] = useState("+91");
  const [phone, setPhone] = useState("");
  const [saved, setSaved] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaved(false);
    await update.mutateAsync({
      first_name: firstName,
      last_name: lastName,
      phone,
      phone_country: phoneCountry,
    });
    setSaved(true);
  };

  return (
    <Card className={styles.card}>
      <Subtitle2>Profile</Subtitle2>
      <Caption1 className={styles.meta}>
        Your display name and contact details. Changes apply immediately.
      </Caption1>
      <form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div className={styles.row}>
          <Field
            label={
              <InfoLabel info="Shown to teammates in mentions, audit events, and the avatar menu. Use the name you go by, not necessarily what's on your ID.">
                First name
              </InfoLabel>
            }
          >
            <Input value={firstName} onChange={(_, d) => setFirstName(d.value)} maxLength={64} />
          </Field>
          <Field
            label={
              <InfoLabel info="Combined with your first name to form the display name used across the product.">
                Last name
              </InfoLabel>
            }
          >
            <Input value={lastName} onChange={(_, d) => setLastName(d.value)} maxLength={64} />
          </Field>
        </div>
        <Field
          label={
            <InfoLabel info="Your login email is locked to the address you signed up with. To change it, ask an admin to invite the new address and remove this account.">
              Email (read-only)
            </InfoLabel>
          }
        >
          <Input value={me.email} disabled />
        </Field>
        <div className={styles.row}>
          <Field
            label={
              <InfoLabel info="E.164 country code (e.g. +1, +44, +91). Used to format the phone number and route SMS verifications.">
                Country code
              </InfoLabel>
            }
          >
            <Input value={phoneCountry} onChange={(_, d) => setPhoneCountry(d.value)} />
          </Field>
          <Field
            label={
              <InfoLabel info="Optional. Used for SMS verification and break-glass account recovery. Never shown to other tenants.">
                Phone (optional)
              </InfoLabel>
            }
          >
            <Input
              type="tel"
              value={phone}
              onChange={(_, d) => setPhone(d.value.replace(/[^\d\s-]/g, ""))}
              maxLength={20}
            />
          </Field>
        </div>
        {saved && (
          <MessageBar intent="success">
            <MessageBarBody>Profile saved.</MessageBarBody>
          </MessageBar>
        )}
        {update.error && (
          <MessageBar intent="error">
            <MessageBarBody>{(update.error as Error).message}</MessageBarBody>
          </MessageBar>
        )}
        <div>
          <Button type="submit" appearance="primary" disabled={update.isPending}>
            {update.isPending ? <Spinner size="tiny" /> : "Save changes"}
          </Button>
        </div>
      </form>
    </Card>
  );
}

function SecurityTab() {
  const styles = useStyles();
  const change = useChangePassword();
  const router = useRouter();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");

  const valid =
    current.length > 0 && next.length >= 8 && next === confirm;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await change.mutateAsync({ current_password: current, new_password: next });
      // Server kills all sessions and clears the cookie — send the user
      // back to /login.
      router.replace("/login?password_changed=1");
    } catch {
      // error message renders inline
    }
  };

  return (
    <Card className={styles.card}>
      <Subtitle2>Change password</Subtitle2>
      <Caption1 className={styles.meta}>
        Confirming the new password signs you out of every device. You'll need
        to sign in again with the new password.
      </Caption1>
      <form onSubmit={submit} style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <Field
          label={
            <InfoLabel info="Required to prove this is really you before we rotate the credential. After three wrong attempts the password change is locked for 15 minutes.">
              Current password
            </InfoLabel>
          }
          required
        >
          <Input type="password" value={current} onChange={(_, d) => setCurrent(d.value)} maxLength={256} />
        </Field>
        <Field
          label={
            <InfoLabel info="Hashed with Argon2id (m=64MB, t=3, p=4). Must be 8+ chars with 3 of: lowercase, uppercase, digit, symbol. Passwords already breached on haveibeenpwned are rejected.">
              New password
            </InfoLabel>
          }
          required
        >
          <Input type="password" value={next} onChange={(_, d) => setNext(d.value)} maxLength={256} />
        </Field>
        <Field
          label={
            <InfoLabel info="Type the new password again exactly. Mismatched values block the submit button to prevent typos that would lock you out.">
              Confirm new password
            </InfoLabel>
          }
          required
        >
          <Input type="password" value={confirm} onChange={(_, d) => setConfirm(d.value)} maxLength={256} />
        </Field>
        {change.error && (
          <MessageBar intent="error">
            <MessageBarBody>{(change.error as Error).message}</MessageBarBody>
          </MessageBar>
        )}
        <div>
          <Button type="submit" appearance="primary" disabled={!valid || change.isPending}>
            {change.isPending ? <Spinner size="tiny" /> : "Change password"}
          </Button>
        </div>
      </form>
    </Card>
  );
}

function SessionsTab() {
  const styles = useStyles();
  const sessions = useAccountSessions();
  const revoke = useRevokeSession();
  const signOutAll = useSignOutEverywhere();
  const router = useRouter();
  const logout = useLogout();

  const handleSignOutAll = async () => {
    if (!confirm("Sign out of every device, including this one?")) return;
    await signOutAll.mutateAsync();
    await logout.mutateAsync().catch(() => {});
    router.replace("/login");
  };

  if (sessions.isLoading) return <LoadingState />;

  return (
    <Card className={styles.card}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <Subtitle2>Active sessions</Subtitle2>
          <Caption1 className={styles.meta}>
            Each row is a device or browser currently signed in.
          </Caption1>
        </div>
        <Button appearance="outline" onClick={handleSignOutAll}>
          Sign out everywhere
        </Button>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {(sessions.data ?? []).map((s) => (
          <div key={s.id} className={styles.sessionRow}>
            <div>
              <Text weight="semibold">
                {s.user_agent || "Unknown device"}{" "}
                {s.current && (
                  <Badge appearance="filled" color="success">
                    this device
                  </Badge>
                )}
              </Text>
              <Caption1 className={styles.meta}>
                {s.ip || "no IP"} · last seen{" "}
                {new Date(s.last_seen_at).toLocaleString()}
              </Caption1>
            </div>
            {!s.current && (
              <Button
                appearance="subtle"
                icon={<Delete20Regular />}
                aria-label="Revoke session"
                onClick={() => revoke.mutate(s.id)}
              />
            )}
          </div>
        ))}
      </div>
    </Card>
  );
}
