"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { makeStyles, tokens } from "@fluentui/react-components";
import { useMe } from "@/lib/auth-client";

const useStyles = makeStyles({
  root: {
    minHeight: "100vh",
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    justifyContent: "center",
    gap: "16px",
    backgroundColor: tokens.colorNeutralBackground2,
  },
  // Plain CSS spinner — deliberately not Fluent's <Spinner> because
  // that component uses useId() internally and produced an SSR-vs-
  // client hydration mismatch on every initial load. The auth-gate is
  // a transient loading state; we don't need the full Fluent affordance
  // for accessibility here (`role="status"` + aria-live is enough).
  ring: {
    width: "40px",
    height: "40px",
    borderRadius: "50%",
    borderTopWidth: "3px",
    borderRightWidth: "3px",
    borderBottomWidth: "3px",
    borderLeftWidth: "3px",
    borderTopStyle: "solid",
    borderRightStyle: "solid",
    borderBottomStyle: "solid",
    borderLeftStyle: "solid",
    borderTopColor: tokens.colorBrandStroke1,
    borderRightColor: tokens.colorNeutralStroke2,
    borderBottomColor: tokens.colorNeutralStroke2,
    borderLeftColor: tokens.colorNeutralStroke2,
    animationName: {
      "0%":   { transform: "rotate(0deg)" },
      "100%": { transform: "rotate(360deg)" },
    },
    animationDuration: "0.9s",
    animationIterationCount: "infinite",
    animationTimingFunction: "linear",
  },
  label: {
    fontSize: "13px",
    color: tokens.colorNeutralForeground2,
  },
});

// RequireAuth gates everything inside the (authed) layout. We let the
// /me query do the work — on 401 the browser is redirected to /login;
// while the query is in flight we render a centered spinner so the page
// doesn't flash partial UI for a logged-out user.
export function RequireAuth({ children }: { children: React.ReactNode }) {
  const styles = useStyles();
  const router = useRouter();
  const { data, isLoading, error } = useMe();

  const unauthenticated =
    !!error && (error as Error & { status?: number }).status === 401;

  useEffect(() => {
    if (unauthenticated) router.replace("/login");
  }, [unauthenticated, router]);

  if (isLoading || unauthenticated || !data) {
    return (
      <div className={styles.root} role="status" aria-live="polite">
        <div className={styles.ring} aria-hidden="true" />
        <div className={styles.label}>Loading workspace…</div>
      </div>
    );
  }
  return <>{children}</>;
}
