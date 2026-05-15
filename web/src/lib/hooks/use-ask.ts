"use client";

import { useMutation } from "@tanstack/react-query";
import { call } from "./_fetch";

// AskGeneration mirrors asker.Generation on the backend — the result
// of /v1/ai:ask. The UI shows generated_sql in a preview pane and
// uses execution_id when the user clicks Run.
export interface AskGeneration {
  execution_id: string;
  generated_sql: string;
  tables_used: string[];
  model: string;
  input_tokens: number;
  output_tokens: number;
}

export interface AskRunResult {
  columns: string[];
  rows: unknown[][];
  row_count: number;
  truncated: boolean;
  elapsed_ms: number;
}

// useAskGenerate runs Text-to-SQL generation. Silent — the page
// renders the SQL preview itself; a toast on success would duplicate
// the visible artefact. Errors do surface as toasts (unsafe SQL, no
// AI provider) since they're terminal states for the user.
export function useAskGenerate() {
  return useMutation({
    mutationFn: (input: { connection_id: string; question: string }) =>
      call<AskGeneration>("POST", "/v1/ai:ask", input),
    meta: { silent: true },
  });
}

// useAskRun executes a previously-generated SQL via the warehouse.
// Silent on success — the results table itself is the feedback.
export function useAskRun(executionId: string | null) {
  return useMutation({
    mutationFn: () => {
      if (!executionId) {
        return Promise.reject(new Error("no execution id"));
      }
      return call<AskRunResult>("POST", `/v1/ai:ask/${executionId}/run`);
    },
    meta: { silent: true },
  });
}
