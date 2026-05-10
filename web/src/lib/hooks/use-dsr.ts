"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

export interface DSRRequest {
  ID: string;
  TenantID: string;
  SubjectID: string;
  Type: "access" | "portability" | "rectification" | "erasure" | "restriction";
  Status: "received" | "processing" | "completed" | "rejected";
  ReceivedAt: string;
  DueAt: string;
  CompletedAt?: string;
  HandledBy?: string;
  Notes?: string;
  ArtifactURN?: string;
}

const KEY = ["dsr"];

import { call } from "./_fetch";

export function useDSRRequests() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const d = await call<{ requests: DSRRequest[] }>("GET", `/v1/dsr`);
      return d.requests ?? [];
    },
  });
}

export function useCreateDSR() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (r: { subject_id: string; type: string; notes?: string }) =>
      call<DSRRequest>("POST", `/v1/dsr`, r),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}

export function useUpdateDSRStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: {
      id: string;
      status: string;
      artifact_urn?: string;
      notes?: string;
    }) =>
      call<DSRRequest>("PATCH", `/v1/dsr/${args.id}/status`, {
        status: args.status,
        artifact_urn: args.artifact_urn,
        notes: args.notes,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  });
}
