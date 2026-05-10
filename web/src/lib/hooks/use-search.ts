"use client";

import { useMutation } from "@tanstack/react-query";
import { call } from "./_fetch";

export interface SemanticHit {
  asset_id: string;
  qualified_name: string;
  type: string;
  description?: string;
  tags?: string[];
  score: number;
}

export interface SemanticResponse {
  query: string;
  hits: SemanticHit[];
}

export function useSemanticSearch() {
  return useMutation({
    mutationFn: (req: { query: string; k?: number }) =>
      call<SemanticResponse>("POST", "/v1/search:semantic", req),
  });
}

// Reindex returns either the legacy synchronous result (embedded mode)
// or {job_id, status, model} (production). Callers handle both via
// `"job_id" in result`.
export interface ReindexEnqueued {
  job_id: string;
  status: string;
  model: string;
}

export interface ReindexResult {
  reindexed: number;
  model: string;
}

export function useReindex() {
  return useMutation({
    mutationFn: () =>
      call<ReindexResult | ReindexEnqueued>("POST", "/v1/search:reindex"),
  });
}
