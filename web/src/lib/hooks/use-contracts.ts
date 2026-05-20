"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export type ContractStatus = "active" | "suspended" | "deprecated";
export type BreachKind = "schema_drift" | "freshness" | "null_threshold";

export interface ExpectedColumn {
  name: string;
  type?: string;
}

export interface Contract {
  id: string;
  tenant_id: string;
  asset_id: string;
  owner_id?: string;
  status: ContractStatus;
  version: number;
  expected_columns?: ExpectedColumn[];
  freshness_seconds?: number;
  null_thresholds?: Record<string, number>;
  description?: string;
  created_at: string;
  updated_at: string;
}

export interface Breach {
  id: string;
  tenant_id: string;
  contract_id: string;
  asset_id: string;
  contract_version: number;
  kind: BreachKind;
  severity: string;
  observed?: Record<string, unknown>;
  expected?: Record<string, unknown>;
  message?: string;
  observed_at: string;
}

export interface UpsertContractInput {
  asset_id: string;
  status?: ContractStatus;
  expected_columns?: ExpectedColumn[];
  freshness_seconds?: number;
  null_thresholds?: Record<string, number>;
  description?: string;
}

export function useContracts() {
  return useQuery({
    queryKey: ["contracts"],
    queryFn: async () => {
      const d = await call<{ contracts: Contract[] }>("GET", "/v1/contracts");
      return d.contracts ?? [];
    },
  });
}

export function useAssetContract(assetId: string | null) {
  return useQuery({
    queryKey: ["asset-contract", assetId],
    enabled: !!assetId,
    queryFn: () =>
      call<Contract | null>("GET", `/v1/assets/${assetId}/contract`),
  });
}

export function useTenantBreaches(limit = 50) {
  return useQuery({
    queryKey: ["contract-breaches", limit],
    queryFn: async () => {
      const d = await call<{ breaches: Breach[] }>(
        "GET",
        `/v1/contracts/breaches?limit=${limit}`,
      );
      return d.breaches ?? [];
    },
  });
}

export function useContractBreaches(contractId: string | null, limit = 50) {
  return useQuery({
    queryKey: ["contract-breaches", contractId, limit],
    enabled: !!contractId,
    queryFn: async () => {
      const d = await call<{ breaches: Breach[] }>(
        "GET",
        `/v1/contracts/${contractId}/breaches?limit=${limit}`,
      );
      return d.breaches ?? [];
    },
  });
}

export function useUpsertContract() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpsertContractInput) =>
      call<Contract>("POST", "/v1/contracts", input),
    onSuccess: (c) => {
      qc.invalidateQueries({ queryKey: ["contracts"] });
      qc.invalidateQueries({ queryKey: ["asset-contract", c.asset_id] });
    },
    meta: { successMessage: "Contract saved" },
  });
}

export function useDeleteContract() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/contracts/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["contracts"] }),
    meta: { successMessage: "Contract deleted" },
  });
}

export function useEvaluateContract() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<{ breaches: Breach[]; count: number }>(
        "POST",
        `/v1/contracts/${id}/evaluate`,
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["contract-breaches"] });
    },
    meta: { successMessage: "Contract evaluated" },
  });
}

export function useEvaluateAllContracts() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      call<{ breach_count: number }>("POST", "/v1/contracts/evaluate"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["contract-breaches"] });
    },
    meta: { successMessage: "All contracts evaluated" },
  });
}
