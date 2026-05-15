"use client";

import Link from "next/link";
import { useState } from "react";
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
import { useForgotPassword } from "@/lib/auth-client";

const useStyles = makeStyles({
  form: { display: "flex", flexDirection: "column", gap: "12px" },
  meta: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
    textAlign: "center",
  },
  link: { color: tokens.colorBrandForeground1, fontWeight: 600, textDecoration: "none" },
});

export default function ForgotPasswordPage() {
  const styles = useStyles();
  const forgot = useForgotPassword();
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);

  const valid = /.+@.+\..+/.test(email);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!valid) return;
    try {
      await forgot.mutateAsync(email.trim().toLowerCase());
      setSent(true);
    } catch {
      // Server always 202s, so an error here is network/parsing only.
      setSent(true);
    }
  };

  if (sent) {
    return (
      <AuthShell
        title="Check your inbox"
        subtitle="If an account exists for that address, a reset link is on its way. The link expires in 24 hours."
      >
        <Link href="/login" className={styles.link}>
          Back to sign in
        </Link>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title="Reset your password"
      subtitle="Tell us the email tied to your account and we'll send a reset link."
    >
      <form className={styles.form} onSubmit={submit} noValidate>
        <Field
          label={
            <InfoLabel info="The address on your account. We always respond 'sent' regardless of whether the address exists — prevents account enumeration. The reset link expires in 24 hours.">
              Work email
            </InfoLabel>
          }
          required
        >
          <Input
            type="email"
            autoComplete="email"
            value={email}
            onChange={(_, d) => setEmail(d.value)}
            disabled={forgot.isPending}
            maxLength={256}
          />
        </Field>
        {forgot.error && (
          <MessageBar intent="error">
            <MessageBarBody>{(forgot.error as Error).message}</MessageBarBody>
          </MessageBar>
        )}
        <Button
          type="submit"
          appearance="primary"
          size="large"
          disabled={!valid || forgot.isPending}
        >
          {forgot.isPending ? <Spinner size="tiny" /> : "Send reset link"}
        </Button>
        <div className={styles.meta}>
          <Link href="/login" className={styles.link}>
            Back to sign in
          </Link>
        </div>
      </form>
    </AuthShell>
  );
}
