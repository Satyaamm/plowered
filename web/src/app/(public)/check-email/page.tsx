"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import {
  Button,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Mail32Filled } from "@fluentui/react-icons";
import { AuthShell } from "@/components/auth-shell";
import { useResendVerification } from "@/lib/auth-client";

const useStyles = makeStyles({
  state: {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    gap: "12px",
    padding: "8px 0",
  },
  icon: { color: tokens.colorBrandForeground1 },
  emph: { fontWeight: 600 },
  meta: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
    textAlign: "center",
  },
  link: { color: tokens.colorBrandForeground1, fontWeight: 600, textDecoration: "none" },
  buttons: { display: "flex", gap: "8px", flexWrap: "wrap", justifyContent: "center" },
});

export default function CheckEmailPage() {
  return (
    <Suspense fallback={null}>
      <CheckEmailInner />
    </Suspense>
  );
}

function CheckEmailInner() {
  const styles = useStyles();
  const params = useSearchParams();
  const email = params.get("email") ?? "";
  const resend = useResendVerification();

  return (
    <AuthShell
      title="Check your email"
      subtitle="We sent a verification link to confirm your account."
    >
      <div className={styles.state}>
        <Mail32Filled className={styles.icon} />
        <p>
          Sent to{" "}
          <span className={styles.emph}>
            {email || "the address you provided"}
          </span>
          .
        </p>
        <p className={styles.meta}>
          Click the link to activate the workspace. The link expires in 24 hours.
        </p>

        <div className={styles.buttons}>
          <Link href="/login">
            <Button appearance="primary">Back to sign-in</Button>
          </Link>
          <Button
            appearance="secondary"
            disabled={!email || resend.isPending || resend.isSuccess}
            onClick={() => email && resend.mutate(email)}
          >
            {resend.isPending ? (
              <Spinner size="tiny" />
            ) : resend.isSuccess ? (
              "Sent"
            ) : (
              "Resend email"
            )}
          </Button>
        </div>

        <p className={styles.meta}>
          Wrong address?{" "}
          <Link href="/signup" className={styles.link}>
            Restart signup
          </Link>
        </p>
      </div>
    </AuthShell>
  );
}
