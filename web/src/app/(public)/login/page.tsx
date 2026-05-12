"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import {
  Button,
  Field,
  InfoLabel,
  Input,
  MessageBar,
  MessageBarBody,
  Spinner,
  makeStyles,
  tokens,
} from "@fluentui/react-components";
import { Eye20Regular, EyeOff20Regular } from "@fluentui/react-icons";
import { AuthShell } from "@/components/auth-shell";
import { useLogin, useResendVerification } from "@/lib/auth-client";

const useStyles = makeStyles({
  form: { display: "flex", flexDirection: "column", gap: "14px" },
  row: { display: "flex", justifyContent: "flex-end" },
  meta: {
    fontSize: "12px",
    color: tokens.colorNeutralForeground3,
    textAlign: "center",
  },
  link: { color: tokens.colorBrandForeground1, fontWeight: 600, textDecoration: "none" },
  iconBtn: {
    border: "none",
    background: "transparent",
    cursor: "pointer",
    padding: "4px",
    color: tokens.colorNeutralForeground3,
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
  },
});

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginInner />
    </Suspense>
  );
}

function LoginInner() {
  const styles = useStyles();
  const router = useRouter();
  const params = useSearchParams();
  const login = useLogin();
  const resend = useResendVerification();

  const [email, setEmail] = useState(params.get("email") ?? "");
  const [password, setPassword] = useState("");
  const [showPw, setShowPw] = useState(false);

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await login.mutateAsync({ email, password });
      router.replace(params.get("next") ?? "/");
    } catch {
      /* error renders below */
    }
  };

  const err = login.error as (Error & { code?: string; status?: number }) | null;
  const needsVerify = err?.code === "email_not_verified";

  return (
    <AuthShell
      title="Sign in"
      subtitle="Welcome back to your data context platform."
    >
      <form className={styles.form} onSubmit={onSubmit}>
        <Field
          label={
            <InfoLabel info="The address you signed up with. If you joined via invitation, this is whichever email the invite was sent to.">
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
            placeholder="you@company.com"
            disabled={login.isPending}
          />
        </Field>
        <Field
          label={
            <InfoLabel info="After 5 wrong attempts the account is rate-limited for 15 minutes. Forgot it? Use 'Forgot password' below — you'll get a one-time reset link.">
              Password
            </InfoLabel>
          }
          required
        >
          <Input
            type={showPw ? "text" : "password"}
            autoComplete="current-password"
            value={password}
            onChange={(_, d) => setPassword(d.value)}
            disabled={login.isPending}
            contentAfter={
              <button
                type="button"
                className={styles.iconBtn}
                onClick={() => setShowPw((v) => !v)}
                aria-label={showPw ? "Hide password" : "Show password"}
              >
                {showPw ? <EyeOff20Regular /> : <Eye20Regular />}
              </button>
            }
          />
        </Field>

        {err && (
          <MessageBar intent={needsVerify ? "warning" : "error"}>
            <MessageBarBody>
              {err.message}
              {needsVerify && (
                <>
                  {" "}
                  <a
                    role="button"
                    style={{ color: "#7C3AED", cursor: "pointer", fontWeight: 600 }}
                    onClick={async () => {
                      await resend.mutateAsync(email);
                    }}
                  >
                    Resend verification email
                  </a>
                  {resend.isSuccess && " — sent."}
                </>
              )}
            </MessageBarBody>
          </MessageBar>
        )}

        <Button
          type="submit"
          appearance="primary"
          size="large"
          disabled={login.isPending || !email || !password}
        >
          {login.isPending ? <Spinner size="tiny" /> : "Sign in"}
        </Button>

        <div className={styles.meta} style={{ display: "flex", justifyContent: "space-between" }}>
          <Link href="/forgot-password" className={styles.link}>
            Forgot password?
          </Link>
          <span>
            New to PurpleCube?{" "}
            <Link href="/signup" className={styles.link}>
              Create a workspace
            </Link>
          </span>
        </div>
      </form>
    </AuthShell>
  );
}
