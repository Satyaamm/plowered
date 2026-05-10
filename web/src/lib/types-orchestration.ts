// Orchestration / quality / notification / policy types.
// Mirrors internal/core/{pipeline,quality,notify,policy} until proto codegen
// produces TS bindings. Kept in a separate file from types.ts so the
// catalog-only pages don't pull these in.

// ---------- pipelines & runs ----------

export type RunStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";
export type TaskStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "skipped"
  | "retrying";

export type TaskType =
  | "sql"
  | "quality_check"
  | "connector_sync"
  | "webhook"
  | "transform_run";

export interface RetryPolicy {
  MaxAttempts?: number;
  InitialBackoff?: number;
  Multiplier?: number;
  MaxBackoff?: number;
}

export interface Schedule {
  Cron: string;
  Timezone?: string;
  Enabled: boolean;
}

export interface Task {
  ID: string;
  Type: TaskType;
  Config?: Record<string, unknown>;
  DependsOn?: string[];
  Retry?: RetryPolicy;
  Timeout?: number;
  Outputs?: string[];
}

export interface Pipeline {
  ID: string;
  TenantID: string;
  Name: string;
  Description?: string;
  Tasks?: Task[];
  Schedule?: Schedule | null;
  Concurrency?: number;
  FailFast?: boolean;
  CreatedAt: string;
  UpdatedAt: string;
  CreatedBy?: string;
  UpdatedBy?: string;
}

export interface Run {
  ID: string;
  TenantID: string;
  PipelineID: string;
  Status: RunStatus;
  StartedAt?: string;
  FinishedAt?: string;
  ScheduledAt: string;
  TriggeredBy?: string;
  IdempotencyKey?: string;
  LastHeartbeat?: string;
}

export interface TaskRun {
  ID: string;
  TenantID: string;
  RunID: string;
  TaskID: string;
  Status: TaskStatus;
  AttemptCount: number;
  StartedAt?: string;
  FinishedAt?: string;
  Error?: string;
  Output?: Record<string, unknown>;
  DeadLetter?: boolean;
}

// ---------- quality ----------

export type CheckType =
  | "row_count"
  | "not_null"
  | "freshness"
  | "uniqueness"
  | "custom_sql";
export type CheckSeverity = "info" | "warning" | "error" | "critical";
export type CheckOutcome = "pass" | "fail" | "error";

export interface Check {
  ID: string;
  TenantID: string;
  AssetID: string;
  AssetQN?: string;
  Name: string;
  Type: CheckType;
  Config?: Record<string, unknown>;
  Severity?: CheckSeverity;
  Owner?: string;
  Enabled: boolean;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface CheckRun {
  ID: string;
  TenantID: string;
  CheckID: string;
  AssetID: string;
  Outcome: CheckOutcome;
  Value: number;
  Threshold: number;
  Diagnostic?: string;
  Properties?: Record<string, unknown>;
  StartedAt: string;
  FinishedAt: string;
  Duration?: number;
  Severity?: CheckSeverity;
}

// ---------- notifications ----------

export type DeliveryStatus = "queued" | "sending" | "delivered" | "failed";

export interface Channel {
  ID: string;
  TenantID: string;
  Kind: string;
  Name: string;
  Config?: Record<string, unknown>;
  SecretURN?: string;
}

export interface NotifyRule {
  ID: string;
  TenantID: string;
  Name?: string;
  ChannelID: string;
  EventTypes?: string[];
  MinSeverity?: string;
  Enabled: boolean;
  CreatedAt: string;
}

export interface Delivery {
  ID: string;
  TenantID: string;
  RuleID: string;
  ChannelID: string;
  EventID?: string;
  Subject?: string;
  Body?: string;
  IdempotencyKey: string;
  Status: DeliveryStatus;
  Attempts: number;
  LastError?: string;
  CreatedAt: string;
  DeliveredAt?: string;
}

// ---------- policies ----------

export type PolicyEffect = "allow" | "deny";
export type PolicyVerb =
  | "read"
  | "edit"
  | "propose"
  | "certify"
  | "delete"
  | "run"
  | "admin";
export type ConditionType =
  | "principal.role"
  | "principal.group"
  | "resource.tag"
  | "resource.owner";

export interface PolicyCondition {
  Type: ConditionType;
  Value: string;
}

export interface PolicyRule {
  ID: string;
  TenantID: string;
  Effect: PolicyEffect;
  Verbs: PolicyVerb[];
  Conditions?: PolicyCondition[];
}
