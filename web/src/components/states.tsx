// Shared loading / empty / error states for list+detail pages. Pages reach
// for these instead of rolling their own so the visual rhythm is identical
// across the app.

"use client";

import { Body1, Spinner, Text, tokens } from "@fluentui/react-components";

export function LoadingState({ label = "Loading…" }: { label?: string }) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: "48px 16px",
      }}
    >
      <Spinner size="small" label={label} />
    </div>
  );
}

export function ErrorBanner({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : "Unknown error";
  return (
    <div
      role="alert"
      style={{
        background: "#F4D6D6",
        color: "#8E1B1B",
        border: "1px solid #C44848",
        borderRadius: 6,
        padding: "10px 14px",
        fontSize: 14,
      }}
    >
      <strong>Something went wrong.</strong> {message}
    </div>
  );
}

export function EmptyState({
  title,
  body,
  action,
}: {
  title: string;
  body?: string;
  action?: React.ReactNode;
}) {
  return (
    <div
      style={{
        textAlign: "center",
        padding: "64px 16px",
        border: `1px dashed ${tokens.colorNeutralStroke2}`,
        borderRadius: 8,
        background: tokens.colorNeutralBackground2,
        display: "flex",
        flexDirection: "column",
        gap: 8,
        alignItems: "center",
      }}
    >
      <Text size={500} weight="semibold">{title}</Text>
      {body && <Body1>{body}</Body1>}
      {action}
    </div>
  );
}
