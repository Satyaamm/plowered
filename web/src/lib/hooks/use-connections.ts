"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export interface Connection {
  id: string;
  name: string;
  type: string;
  config: Record<string, unknown>;
  health: "unknown" | "healthy" | "degraded" | "unreachable";
  last_check_at?: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface CreateConnectionInput {
  name: string;
  type: string;
  config: Record<string, unknown>;
  password?: string;
}

export interface TestConnectionResult {
  ok: boolean;
  health: string;
  checked_at: string;
  error?: string;
}

const KEY = ["connections"];

export function useConnections() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const d = await call<{ connections: Connection[] }>("GET", "/v1/connections");
      return d.connections ?? [];
    },
  });
}

export function useCreateConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateConnectionInput) =>
      call<Connection>("POST", "/v1/connections", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Connection created" },
  });
}

export function useDeleteConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/connections/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Connection deleted" },
  });
}

export function useTestConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<TestConnectionResult>("POST", `/v1/connections/${id}/test`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Connection healthy" },
  });
}

// useTestDraftConnection runs the handshake against an unsaved form
// payload (same shape as create). Used by the wizard so the user
// validates credentials before we ever persist them. Silent — the
// wizard renders inline result UI; a toast on every form keystroke
// would be noise.
export function useTestDraftConnection() {
  return useMutation({
    mutationFn: (body: CreateConnectionInput) =>
      call<TestConnectionResult>("POST", "/v1/connections:test", body),
    meta: { silent: true },
  });
}

export interface CrawlAck {
  status: string;
  connection_id: string;
  queued_at: string;
}

export function useCrawlConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<CrawlAck>("POST", `/v1/connections/${id}/crawl`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
      qc.invalidateQueries({ queryKey: ["assets"] });
    },
    meta: { successMessage: "Crawl queued" },
  });
}

// ClassifyResult is the legacy synchronous response shape — kept so the
// embedded/dev path (no Jobs wired) still typechecks. Production returns
// a 202 + ClassifyEnqueued, which the UI then polls via useJob.
export interface ClassifyResult {
  tables: number;
  columns: number;
  tagged: number;
  skipped: number;
}

export interface ClassifyEnqueued {
  job_id: string;
  status: string;
  resource_id: string;
}

export function useClassifyConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<ClassifyResult | ClassifyEnqueued>(
        "POST",
        `/v1/connections/${id}/classify`,
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["assets"] });
    },
    meta: { successMessage: "Classification queued" },
  });
}

// ---- two-phase classify ------------------------------------------------

export interface ClassifyProposalColumn {
  asset_id: string;
  name: string;
  sampled: number;
  hits: Record<string, number>;
  proposed_tags: string[];
}

export interface ClassifyProposalTable {
  asset_id: string;
  schema: string;
  name: string;
  columns: ClassifyProposalColumn[];
}

export interface ClassifyProposalSkip {
  table: string;
  reason: string;
}

export interface ClassifyProposal {
  tables: ClassifyProposalTable[];
  skipped: ClassifyProposalSkip[];
}

export interface ClassifyPreviewRequest {
  schemas?: string[];
  tables?: string[];
}

// useClassifyPreview runs the sampler over the connection and returns
// proposed tags WITHOUT writing anything. The wizard's review step
// renders this output and lets the operator accept/reject per column.
// Silent — the wizard transitions to the review screen on success and
// shows an inline ErrorBanner on failure; a toast would duplicate.
export function useClassifyPreview(connectionId: string) {
  return useMutation({
    mutationFn: (body: ClassifyPreviewRequest) =>
      call<ClassifyProposal>(
        "POST",
        `/v1/connections/${connectionId}/classify:preview`,
        body,
      ),
    meta: { silent: true },
  });
}

export interface ClassifyDecision {
  column_asset_id: string;
  tags: string[];
}

export interface ClassifyApplyResult {
  applied: number;
  columns_updated: number;
}

export interface ConnectionScopeTable {
  schema: string;
  name: string;
  asset_id: string;
}

export interface ConnectionScope {
  schemas: string[];
  tables: ConnectionScopeTable[];
}

// useConnectionScope returns the schemas + tables the catalog knows
// about for this connection. Powers the classify wizard's dropdowns so
// the operator picks from real names instead of typing freely.
export function useConnectionScope(connectionId: string) {
  return useQuery({
    queryKey: ["connection-scope", connectionId],
    queryFn: () =>
      call<ConnectionScope>("GET", `/v1/connections/${connectionId}/scope`),
    enabled: !!connectionId,
    staleTime: 60_000,
  });
}

// useClassifyApply persists the operator's approved tags. Tags omitted
// from the decisions list are NOT written, even if the preview
// proposed them.
export function useClassifyApply(connectionId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (decisions: ClassifyDecision[]) =>
      call<ClassifyApplyResult>(
        "POST",
        `/v1/connections/${connectionId}/classify:apply`,
        { decisions },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["assets"] });
    },
    meta: { successMessage: "Classifications applied" },
  });
}
