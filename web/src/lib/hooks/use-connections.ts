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
  });
}

export function useDeleteConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/connections/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}

export function useTestConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<TestConnectionResult>("POST", `/v1/connections/${id}/test`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
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
  });
}
