"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect } from "react";
import {
  Button,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import {
  CheckmarkCircle32Filled,
  DismissCircle32Filled,
} from "@fluentui/react-icons";
import { AuthShell } from "@/components/auth-shell";
import { useVerifyEmail } from "@/lib/auth-client";

const useStyles = makeStyles({
  state: {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    gap: "16px",
    padding: "8px 0",
  },
  ok: { color: tokens.colorPaletteGreenForeground2 },
  err: { color: tokens.colorPaletteRedForeground1 },
  msg: { textAlign: "center", color: tokens.colorNeutralForeground2 },
});

export default function VerifyPage() {
  return (
    <Suspense fallback={null}>
      <VerifyInner />
    </Suspense>
  );
}

function VerifyInner() {
  const styles = useStyles();
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get("token") ?? "";
  const verify = useVerifyEmail();

  useEffect(() => {
    if (token) verify.mutate(token);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  if (!token) {
    return (
      <AuthShell title="Verify email" subtitle="Missing token.">
        <div className={styles.state}>
          <DismissCircle32Filled className={styles.err} />
          <p className={styles.msg}>The verification link is missing its token.</p>
          <Link href="/login">
            <Button appearance="primary">Back to sign-in</Button>
          </Link>
        </div>
      </AuthShell>
    );
  }

  if (verify.isPending || verify.isIdle) {
    return (
      <AuthShell title="Verifying…" subtitle="Confirming your email with the platform.">
        <div className={styles.state}>
          <Spinner size="large" />
        </div>
      </AuthShell>
    );
  }

  if (verify.isError) {
    return (
      <AuthShell
        title="Verification failed"
        subtitle="The link is invalid, already used, or expired."
      >
        <div className={styles.state}>
          <DismissCircle32Filled className={styles.err} />
          <p className={styles.msg}>{(verify.error as Error).message}</p>
          <Link href="/login">
            <Button appearance="primary">Back to sign-in</Button>
          </Link>
        </div>
      </AuthShell>
    );
  }

  return (
    <AuthShell
      title="You're verified"
      subtitle="Your email is confirmed. Sign in to access the workspace."
    >
      <div className={styles.state}>
        <CheckmarkCircle32Filled className={styles.ok} />
        <p className={styles.msg}>{verify.data?.message ?? "Email verified."}</p>
        <Button appearance="primary" onClick={() => router.replace("/login")}>
          Continue to sign-in
        </Button>
      </div>
    </AuthShell>
  );
}
