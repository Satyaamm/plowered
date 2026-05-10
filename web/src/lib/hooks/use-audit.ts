"use client";

import { useQuery } from "@tanstack/react-query";
import { call } from "./_fetch";

export interface AuditEvent {
  event_id: string;
  tenant_id: string;
  actor_id: string;
  actor_kind: string;
  action: string;
  resource_type: string;
  resource_id: string;
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  ip?: string;
  user_agent?: string;
  request_id?: string;
  outcome?: string;
  http_method?: string;
  http_path?: string;
  http_status?: number;
  created_at: string;
}

export function useAuditFeed(limit = 200) {
  return useQuery({
    queryKey: ["audit", limit],
    queryFn: async () => {
      const body = await call<{ events?: AuditEvent[] }>(
        "GET",
        `/v1/audit?limit=${limit}`,
      );
      return body.events ?? [];
    },
    refetchInterval: 15_000,
  });
}
