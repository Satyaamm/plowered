"use client";

import Link from "next/link";
import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Body1,
  Button,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Spinner,
  Text,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { AuthShell } from "@/components/auth-shell";
import { useLogin } from "@/lib/auth-client";
import { useAcceptInvite, useInviteInfo } from "@/lib/hooks";

const useStyles = makeStyles({
  form: { display: "flex", flexDirection: "column", gap: "14px" },
  meta: { color: tokens.colorNeutralForeground3, fontSize: "12px" },
});

function AcceptInviteInner() {
  const styles = useStyles();
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get("token");

  const info = useInviteInfo(token);
  const accept = useAcceptInvite();
  const login = useLogin();
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [err, setErr] = useState<string | null>(null);

  if (!token) {
    return (
      <AuthShell
        title="Invite link missing"
        subtitle="The /accept-invite link must include a token query param."
      >
        <Link href="/login">Back to sign in</Link>
      </AuthShell>
    );
  }

  if (info.isLoading) {
    return (
      <AuthShell title="Loading invite…">
        <Spinner />
      </AuthShell>
    );
  }

  if (info.error) {
    return (
      <AuthShell
        title="Invite no longer valid"
        subtitle="The link may have expired, already been used, or been revoked. Ask your admin for a fresh invite."
      >
        <Link href="/login">Back to sign in</Link>
      </AuthShell>
    );
  }

  const data = info.data!;

  const valid =
    password.length >= 8 && password === confirm && firstName.trim() !== "";

  const submit = async () => {
    setErr(null);
    try {
      await accept.mutateAsync({
        token,
        password,
        first_name: firstName,
        last_name: lastName,
      });
      // Auto-login then send to the dashboard.
      try {
        await login.mutateAsync({ email: data.email, password });
        router.push("/");
      } catch {
        router.push("/login?invited=1");
      }
    } catch (e: unknown) {
      const error = e as { message?: string };
      setErr(error.message ?? "could not accept invite");
    }
  };

  return (
    <AuthShell
      title={`Join ${data.workspace_name}`}
      subtitle={`You were invited to join as ${data.roles.join(", ")}.`}
    >
      <div className={styles.form}>
        <Field label="Work email">
          <Input value={data.email} disabled />
        </Field>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
          <Field label="First name" required>
            <Input value={firstName} onChange={(_, d) => setFirstName(d.value)} />
          </Field>
          <Field label="Last name">
            <Input value={lastName} onChange={(_, d) => setLastName(d.value)} />
          </Field>
        </div>
        <Field
          label="Password"
          required
          hint="8+ characters, at least three of: uppercase, lowercase, digit, symbol."
        >
          <Input
            type="password"
            value={password}
            onChange={(_, d) => setPassword(d.value)}
          />
        </Field>
        <Field label="Confirm password" required>
          <Input
            type="password"
            value={confirm}
            onChange={(_, d) => setConfirm(d.value)}
          />
        </Field>

        {err && (
          <MessageBar intent="error">
            <MessageBarBody>{err}</MessageBarBody>
          </MessageBar>
        )}

        <Button
          appearance="primary"
          onClick={submit}
          disabled={!valid || accept.isPending}
        >
          {accept.isPending ? <Spinner size="extra-tiny" /> : "Join workspace"}
        </Button>
        <Body1 className={styles.meta}>
          <Text>
            Already have an account?{" "}
            <Link href={`/login?invite=${encodeURIComponent(token)}`}>
              sign in
            </Link>{" "}
            and we'll attach this workspace to it.
          </Text>
        </Body1>
      </div>
    </AuthShell>
  );
}

export default function AcceptInvitePage() {
  return (
    <Suspense fallback={<AuthShell title="Loading…"><Spinner /></AuthShell>}>
      <AcceptInviteInner />
    </Suspense>
  );
}
