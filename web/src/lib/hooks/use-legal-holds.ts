"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

export interface LegalHold {
  ID: string;
  TenantID: string;
  Matter: string;
  Reason: string;
  Scope: {
    resource_types?: string[];
    resource_ids?: string[];
    tags?: string[];
  };
  IssuedBy: string;
  IssuedAt: string;
  ReleasedAt: string;
}

const KEY = ["legal-holds"];

import { call } from "./_fetch";

export function useLegalHolds() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const d = await call<{ holds: LegalHold[] }>("GET", `/v1/legal-holds`);
      return d.holds ?? [];
    },
  });
}

export function useIssueLegalHold() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (h: Partial<LegalHold>) =>
      call<LegalHold>("POST", `/v1/legal-holds`, h),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Legal hold issued" },
  });
}

export function useReleaseLegalHold() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("POST", `/v1/legal-holds/${id}/release`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Legal hold released" },
  });
}
