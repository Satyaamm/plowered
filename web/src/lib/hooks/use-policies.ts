"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { policiesApi } from "@/lib/api";
import type { PolicyRule } from "@/lib/types-orchestration";

const KEY = ["policies"];

export function usePolicies() {
  return useQuery({
    queryKey: KEY,
    queryFn: ({ signal }) => policiesApi.list({ signal }),
    select: (d) => d.rules ?? [],
  });
}

export function useCreatePolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (r: Partial<PolicyRule>) => policiesApi.create(r),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}

export function useDeletePolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => policiesApi.remove(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}
