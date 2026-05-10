"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { Spinner, makeStyles, tokens } from "@fluentui/react-components";
import { useMe } from "@/lib/auth-client";

const useStyles = makeStyles({
  root: {
    minHeight: "100vh",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: tokens.colorNeutralBackground2,
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
      <div className={styles.root}>
        <Spinner size="large" label="Loading workspace…" />
      </div>
    );
  }
  return <>{children}</>;
}
