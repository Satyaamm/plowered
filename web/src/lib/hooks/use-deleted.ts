"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

export interface DeletedRecord {
  ID: string;
  TenantID: string;
  ResourceType: string;
  ResourceID: string;
  Payload: Record<string, unknown>;
  DeletedBy: string;
  DeletedKind: string;
  DeletionReason: string;
  RequestID?: string;
  ParentTombstoneID?: string;
  DeletedAt: string;
  RestoredAt?: string;
  RestoredBy?: string;
  PurgedAt?: string;
  PurgedBy?: string;
}

const KEY = ["deleted"];

import { call } from "./_fetch";

export function useDeleted(opts?: { resourceType?: string; limit?: number }) {
  return useQuery({
    queryKey: [...KEY, opts?.resourceType ?? "all", opts?.limit ?? 100],
    queryFn: async () => {
      const q = new URLSearchParams();
      if (opts?.resourceType) q.set("type", opts.resourceType);
      if (opts?.limit) q.set("limit", String(opts.limit));
      const qs = q.toString();
      const data = await call<{ records: DeletedRecord[] }>(
        "GET",
        `/v1/deleted${qs ? "?" + qs : ""}`,
      );
      return data.records ?? [];
    },
  });
}

export function useRestoreRecord() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<{ status: string }>("POST", `/v1/deleted/${id}/restore`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}

export function usePurgeRecord() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/deleted/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}
