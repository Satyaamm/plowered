"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { pipelinesApi } from "@/lib/api";
import type { Pipeline } from "@/lib/types-orchestration";

const KEY = ["pipelines"];

export function usePipelines() {
  return useQuery({
    queryKey: KEY,
    queryFn: ({ signal }) => pipelinesApi.list({ signal }),
    select: (d) => d.pipelines ?? [],
  });
}

export function usePipeline(id: string | undefined) {
  return useQuery({
    queryKey: ["pipeline", id],
    queryFn: ({ signal }) => pipelinesApi.get(id!, { signal }),
    enabled: !!id,
  });
}

export function useCreatePipeline() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: Partial<Pipeline>) => pipelinesApi.create(p),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Pipeline created" },
  });
}

export function useUpdatePipeline(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: Partial<Pipeline>) => pipelinesApi.update(id, p),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
      qc.invalidateQueries({ queryKey: ["pipeline", id] });
    },
    meta: { successMessage: "Pipeline saved" },
  });
}

export function useDeletePipeline() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => pipelinesApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Pipeline deleted" },
  });
}

export function useTriggerPipeline() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => pipelinesApi.trigger(id),
    onSuccess: (_run, id) => {
      qc.invalidateQueries({ queryKey: ["runs"] });
      qc.invalidateQueries({ queryKey: ["pipeline-runs", id] });
    },
    meta: { successMessage: "Pipeline run started" },
  });
}
