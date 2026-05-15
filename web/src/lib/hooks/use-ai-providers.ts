"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export type AIProviderKind =
  | "anthropic"
  | "openai"
  | "deepseek"
  | "openai-compatible";

export type AICapability = "chat" | "embed";

export interface AIProvider {
  id: string;
  kind: AIProviderKind;
  name: string;
  model: string;
  base_url?: string;
  is_primary: boolean;
  capability: AICapability;
  created_at: string;
  updated_at: string;
  last_tested_at?: string;
  last_test_ok: boolean;
  last_test_error?: string;
}

export interface AIProviderInput {
  kind: AIProviderKind;
  name: string;
  model: string;
  base_url?: string;
  api_key?: string;
  capability: AICapability;
  is_primary?: boolean;
}

export interface TestResult {
  ok: boolean;
  error?: string;
}

const KEY = ["ai-providers"];

export function useAIProviders() {
  return useQuery({
    queryKey: KEY,
    queryFn: () =>
      call<{ providers: AIProvider[] }>("GET", "/v1/ai/providers").then(
        (r) => r.providers ?? [],
      ),
  });
}

export function useCreateAIProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AIProviderInput) =>
      call<AIProvider>("POST", "/v1/ai/providers", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
    },
    meta: { successMessage: "AI provider added" },
  });
}

export function useUpdateAIProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: AIProviderInput & { id: string }) =>
      call<AIProvider>("PATCH", `/v1/ai/providers/${id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
    },
    meta: { successMessage: "AI provider saved" },
  });
}

export function useDeleteAIProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<void>("DELETE", `/v1/ai/providers/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
    },
    meta: { successMessage: "AI provider removed" },
  });
}

// useTestInlineAIProvider powers the "Test" button before save. Sends
// the full draft payload (including the api_key) and gets back ok/error
// without persisting anything. Silent — the result lives inline in the
// form; a toast on every form-blur would be noise.
export function useTestInlineAIProvider() {
  return useMutation({
    mutationFn: (body: AIProviderInput) =>
      call<TestResult>("POST", "/v1/ai/providers:test", body),
    meta: { silent: true },
  });
}

// useTestStoredAIProvider re-probes credentials already on file. Used
// for the per-row "Test" action on the list page.
export function useTestStoredAIProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<TestResult>("POST", `/v1/ai/providers/${id}/test`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
    },
    meta: { successMessage: "Provider reachable" },
  });
}

export function useSetPrimaryAIProvider() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<void>("POST", `/v1/ai/providers/${id}/primary`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: KEY });
    },
    meta: { successMessage: "Primary provider updated" },
  });
}

// Recommended-model menu the form's combobox renders. Empty means the
// user types a free-form model id.
export const SUGGESTED_MODELS: Record<AIProviderKind, string[]> = {
  anthropic: [
    "claude-opus-4-7",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
  ],
  openai: [
    "gpt-4o",
    "gpt-4o-mini",
    "text-embedding-3-small",
    "text-embedding-3-large",
  ],
  deepseek: ["deepseek-chat", "deepseek-reasoner"],
  "openai-compatible": [],
};
