"use client";

import { useQuery } from "@tanstack/react-query";
import { call } from "./_fetch";

export type CostKind = "ai_completion" | "warehouse_query" | string;

export interface CostRecord {
  id: string;
  tenant_id: string;
  ts: string;
  kind: CostKind;
  provider: string;
  cost_usd: number;
  attributes?: Record<string, unknown>;
}

export interface CostDailyTotal {
  day: string;
  kind: CostKind;
  provider: string;
  cost_usd: number;
  count: number;
}

export interface CostSummaryResponse {
  daily: CostDailyTotal[];
  by_kind: Record<string, number>;
  by_provider: Record<string, number>;
  total_usd: number;
}

export function useCostSummary(rangeDays = 30) {
  return useQuery({
    queryKey: ["cost-summary", rangeDays],
    queryFn: () => {
      const to = new Date();
      const from = new Date();
      from.setUTCDate(from.getUTCDate() - rangeDays);
      const q = new URLSearchParams({
        from: from.toISOString(),
        to: to.toISOString(),
      });
      return call<CostSummaryResponse>("GET", `/v1/cost/summary?${q}`);
    },
  });
}

export function useRecentCost(limit = 100) {
  return useQuery({
    queryKey: ["cost-recent", limit],
    queryFn: async () => {
      const d = await call<{ records: CostRecord[] }>(
        "GET",
        `/v1/cost/recent?limit=${limit}`,
      );
      return d.records ?? [];
    },
  });
}
