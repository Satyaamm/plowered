"use client";

import { useQuery } from "@tanstack/react-query";
import { runsApi } from "@/lib/api";

/**
 * useRuns lists recent runs, optionally filtered by pipeline.
 * Polls every 5s when a run in the result set is non-terminal so the UI
 * reflects status changes without requiring SSE.
 */
export function useRuns(params?: { pipelineId?: string; limit?: number }) {
  return useQuery({
    queryKey: ["runs", params?.pipelineId ?? "all", params?.limit ?? 50],
    queryFn: ({ signal }) => runsApi.list(params, { signal }),
    select: (d) => d.runs ?? [],
    refetchInterval: (q) => {
      const runs = q.state.data?.runs ?? [];
      return runs.some((r) => r.Status === "queued" || r.Status === "running")
        ? 5_000
        : false;
    },
  });
}

export function useRun(id: string | undefined) {
  return useQuery({
    queryKey: ["run", id],
    queryFn: ({ signal }) => runsApi.get(id!, { signal }),
    enabled: !!id,
    refetchInterval: (q) => {
      const r = q.state.data;
      return r && (r.Status === "queued" || r.Status === "running")
        ? 3_000
        : false;
    },
  });
}

export function useTaskRuns(runId: string | undefined) {
  return useQuery({
    queryKey: ["run-tasks", runId],
    queryFn: ({ signal }) => runsApi.tasks(runId!, { signal }),
    enabled: !!runId,
    select: (d) => d.task_runs ?? [],
    refetchInterval: 3_000,
  });
}
