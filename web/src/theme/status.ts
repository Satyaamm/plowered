// Status color tokens for orchestration & quality views. Reference these
// from any chip/badge/icon — never hard-code hex values.
//
// Tokens were chosen to read well against the Loamy cream canvas
// (#FAF6F0). All foreground/background pairs meet WCAG AA contrast.

import {
  type RunStatus,
  type TaskStatus,
  type CheckOutcome,
  type DeliveryStatus,
} from "@/lib/types-orchestration";

export interface StatusColor {
  fg: string; // text + icon
  bg: string; // pill background
  border: string;
  label: string;
}

export const runStatusColors: Record<RunStatus, StatusColor> = {
  queued:    { label: "Queued",    fg: "#6E3611", bg: "#F3D2BC", border: "#DF9762" },
  running:   { label: "Running",   fg: "#15558A", bg: "#D6E5F4", border: "#4F89BD" },
  succeeded: { label: "Succeeded", fg: "#1F6334", bg: "#D6EBD9", border: "#4FA269" },
  failed:    { label: "Failed",    fg: "#8E1B1B", bg: "#F4D6D6", border: "#C44848" },
  cancelled: { label: "Cancelled", fg: "#5C4F3C", bg: "#E5DFD3", border: "#B0A790" },
};

export const taskStatusColors: Record<TaskStatus, StatusColor> = {
  queued:    runStatusColors.queued,
  running:   runStatusColors.running,
  succeeded: runStatusColors.succeeded,
  failed:    runStatusColors.failed,
  skipped:   { label: "Skipped",  fg: "#4F4F4F", bg: "#E8E4DD", border: "#A8A29A" },
  retrying:  { label: "Retrying", fg: "#7A4E14", bg: "#F7E2D4", border: "#D27C44" },
};

export const checkOutcomeColors: Record<CheckOutcome, StatusColor> = {
  pass:  runStatusColors.succeeded,
  fail:  runStatusColors.failed,
  error: { label: "Error", fg: "#7A4E14", bg: "#F7E2D4", border: "#D27C44" },
};

export const deliveryStatusColors: Record<DeliveryStatus, StatusColor> = {
  queued:    runStatusColors.queued,
  sending:   runStatusColors.running,
  delivered: runStatusColors.succeeded,
  failed:    runStatusColors.failed,
};

/** Fallback used when an API returns a status we don't know yet. */
export const unknownStatusColor: StatusColor = {
  label: "Unknown",
  fg: "#4F4F4F",
  bg: "#E8E4DD",
  border: "#A8A29A",
};
