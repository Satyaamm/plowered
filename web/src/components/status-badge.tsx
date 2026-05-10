// StatusBadge renders a small pill for run / task / check / delivery
// status. Always reaches into the theme/status tokens — never accepts a
// raw color prop, so the brand stays consistent.

"use client";

import {
  checkOutcomeColors,
  deliveryStatusColors,
  runStatusColors,
  taskStatusColors,
  unknownStatusColor,
  type StatusColor,
} from "@/theme/status";
import type {
  CheckOutcome,
  DeliveryStatus,
  RunStatus,
  TaskStatus,
} from "@/lib/types-orchestration";

type Variant = "run" | "task" | "check" | "delivery";

export interface StatusBadgeProps {
  variant: Variant;
  status: RunStatus | TaskStatus | CheckOutcome | DeliveryStatus | string;
}

function pickColor(variant: Variant, status: string): StatusColor {
  switch (variant) {
    case "run":
      return runStatusColors[status as RunStatus] ?? unknownStatusColor;
    case "task":
      return taskStatusColors[status as TaskStatus] ?? unknownStatusColor;
    case "check":
      return checkOutcomeColors[status as CheckOutcome] ?? unknownStatusColor;
    case "delivery":
      return deliveryStatusColors[status as DeliveryStatus] ?? unknownStatusColor;
  }
}

export function StatusBadge({ variant, status }: StatusBadgeProps) {
  const c = pickColor(variant, status);
  return (
    <span
      style={{
        display: "inline-flex",
        alignItems: "center",
        gap: 6,
        padding: "2px 10px",
        borderRadius: 999,
        fontSize: 12,
        fontWeight: 500,
        color: c.fg,
        background: c.bg,
        border: `1px solid ${c.border}`,
        lineHeight: 1.6,
      }}
      aria-label={`${variant} status: ${c.label}`}
    >
      <span
        aria-hidden
        style={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          background: c.fg,
          display: "inline-block",
        }}
      />
      {c.label}
    </span>
  );
}
