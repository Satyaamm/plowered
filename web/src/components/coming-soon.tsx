"use client";

import { Body1, Title3, tokens } from "@fluentui/react-components";
import { Sparkle24Regular } from "@fluentui/react-icons";

export function ComingSoon({ what }: { what: string }) {
  return (
    <div
      style={{
        padding: "32px",
        backgroundColor: tokens.colorNeutralBackground1,
        border: `1px dashed ${tokens.colorNeutralStroke2}`,
        borderRadius: "8px",
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: "10px",
        maxWidth: "720px",
      }}
    >
      <span
        style={{
          width: "36px",
          height: "36px",
          borderRadius: "6px",
          backgroundColor: "#FEF4E8",
          color: tokens.colorBrandForeground1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <Sparkle24Regular />
      </span>
      <Title3>{what}</Title3>
      <Body1>
        The schema and policy hooks are in place; the operator surface is the next
        increment. Visit the API directly via <code>/v1</code> in the meantime.
      </Body1>
    </div>
  );
}
