"use client";

import { useQuery } from "@tanstack/react-query";
import { call } from "./_fetch";

export interface PlatformStats {
  catalog: {
    total: number;
    by_type: Record<string, number>;
    tagged: number;
  };
  pipelines: number;
  checks: number;
  failing_checks: number;
  deleted_active: number;
  holds_active: number;
  dsr_open: number;
  connections: number;
  healthy_connections: number;
}

export function useStats() {
  return useQuery({
    queryKey: ["stats"],
    queryFn: () => call<PlatformStats>("GET", "/v1/stats"),
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
}
