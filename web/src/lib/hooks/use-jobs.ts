"use client";

import { useQuery } from "@tanstack/react-query";
import { call } from "./_fetch";

export type JobStatus =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "cancelled";

export interface Job {
  id: string;
  type: string;
  status: JobStatus;
  progress_pct: number;
  message?: string;
  result?: Record<string, unknown>;
  error?: string;
  resource_id?: string;
  actor_id?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

// useJob polls /v1/jobs/{id} every 2s while the job is in flight; once it
// reaches a terminal status (succeeded/failed/cancelled) the poll stops.
// Pass null/undefined to disable the query.
export function useJob(id: string | null | undefined) {
  return useQuery({
    queryKey: ["jobs", id],
    enabled: !!id,
    queryFn: () => call<Job>("GET", `/v1/jobs/${id}`),
    refetchInterval: (query) => {
      const j = query.state.data as Job | undefined;
      if (!j) return 2000;
      if (j.status === "queued" || j.status === "running") return 2000;
      return false;
    },
    staleTime: 0,
  });
}

export function useJobsList(limit = 50) {
  return useQuery({
    queryKey: ["jobs", "list", limit],
    queryFn: () => call<{ jobs: Job[] }>("GET", `/v1/jobs?limit=${limit}`),
  });
}
