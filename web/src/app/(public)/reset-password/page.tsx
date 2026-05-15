"use client";

import Link from "next/link";
import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  Button,
  Field,
  Input,
  MessageBar,
  MessageBarBody,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { AuthShell } from "@/components/auth-shell";
import { InfoLabel } from "@/components/info-label";
import { useResetPassword } from "@/lib/auth-client";

const useStyles = makeStyles({
  form: { display: "flex", flexDirection: "column", gap: "12px" },
  hint: { fontSize: "11px", color: tokens.colorNeutralForeground3 },
  link: { color: tokens.colorBrandForeground1, fontWeight: 600, textDecoration: "none" },
});

function ResetInner() {
  const styles = useStyles();
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get("token");
  const reset = useResetPassword();
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [err, setErr] = useState<string | null>(null);

  if (!token) {
    return (
      <AuthShell
        title="Reset link missing"
        subtitle="The reset link must include a token. Request a new one from the forgot-password page."
      >
        <Link href="/forgot-password" className={styles.link}>
          Request a new link
        </Link>
      </AuthShell>
    );
  }

  const valid = password.length >= 8 && password === confirm;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null);
    try {
      await reset.mutateAsync({ token, password });
      router.replace("/login?reset=1");
    } catch (e: unknown) {
      const error = e as { message?: string };
      setErr(error.message ?? "could not reset password");
    }
  };

  return (
    <AuthShell
      title="Choose a new password"
      subtitle="8+ chars with at least three of: lowercase, uppercase, digit, symbol."
    >
      <form className={styles.form} onSubmit={submit} noValidate>
        <Field
          label={
            <InfoLabel info="Hashed with Argon2id — never stored in plaintext. 8+ chars with 3 of: lowercase, uppercase, digit, symbol. Resetting signs you out of every other device immediately.">
              New password
            </InfoLabel>
          }
          required
        >
          <Input
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(_, d) => setPassword(d.value)}
            maxLength={256}
            disabled={reset.isPending}
          />
        </Field>
        <Field
          label={
            <InfoLabel info="Re-enter the new password exactly. The submit button stays disabled until both fields match — guards against typos that would otherwise lock you out.">
              Confirm new password
            </InfoLabel>
          }
          required
        >
          <Input
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(_, d) => setConfirm(d.value)}
            maxLength={256}
            disabled={reset.isPending}
          />
        </Field>
        {err && (
          <MessageBar intent="error">
            <MessageBarBody>{err}</MessageBarBody>
          </MessageBar>
        )}
        <Button
          type="submit"
          appearance="primary"
          size="large"
          disabled={!valid || reset.isPending}
        >
          {reset.isPending ? <Spinner size="tiny" /> : "Reset password"}
        </Button>
      </form>
    </AuthShell>
  );
}

export default function ResetPasswordPage() {
  return (
    <Suspense fallback={<AuthShell title="Loading…"><Spinner /></AuthShell>}>
      <ResetInner />
    </Suspense>
  );
}
