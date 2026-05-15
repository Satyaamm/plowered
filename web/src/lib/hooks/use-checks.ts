"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { checksApi } from "@/lib/api";
import type { Check } from "@/lib/types-orchestration";

const LIST_KEY = ["checks"];

export function useChecks(params?: { assetId?: string }) {
  return useQuery({
    queryKey: [...LIST_KEY, params?.assetId ?? "all"],
    queryFn: ({ signal }) => checksApi.list(params, { signal }),
    select: (d) => d.checks ?? [],
  });
}

export function useCheck(id: string | undefined) {
  return useQuery({
    queryKey: ["check", id],
    queryFn: ({ signal }) => checksApi.get(id!, { signal }),
    enabled: !!id,
  });
}

export function useCheckRuns(id: string | undefined, limit = 50) {
  return useQuery({
    queryKey: ["check-runs", id, limit],
    queryFn: ({ signal }) => checksApi.runs(id!, { limit }, { signal }),
    enabled: !!id,
    select: (d) => d.runs ?? [],
    refetchInterval: 10_000,
  });
}

export function useCreateCheck() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: Partial<Check>) => checksApi.create(c),
    onSuccess: () => qc.invalidateQueries({ queryKey: LIST_KEY }),
    meta: { successMessage: "Check created" },
  });
}

export function useUpdateCheck(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: Partial<Check>) => checksApi.update(id, c),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: LIST_KEY });
      qc.invalidateQueries({ queryKey: ["check", id] });
    },
    meta: { successMessage: "Check saved" },
  });
}

export function useDeleteCheck() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => checksApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: LIST_KEY }),
    meta: { successMessage: "Check deleted" },
  });
}

export function useRunCheck() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: {
      id: string;
      sourceId?: string;
      samplePercent?: number;
      timeoutSec?: number;
    }) =>
      checksApi.trigger(args.id, {
        source_id: args.sourceId,
        sample_percent: args.samplePercent,
        timeout_sec: args.timeoutSec,
      }),
    onSuccess: (_data, args) => {
      qc.invalidateQueries({ queryKey: ["check-runs", args.id] });
    },
    meta: { successMessage: "Check run started" },
  });
}
