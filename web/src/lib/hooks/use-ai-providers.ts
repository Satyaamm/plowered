"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

// Keep this list in lockstep with internal/core/aiprovider/aiprovider.go
// AllKinds order. Adding a new kind here without adding the matching
// backend adapter will surface a "unknown kind" error on Test.
export type AIProviderKind =
  | "anthropic"
  | "openai"
  | "gemini"
  | "azure-openai"
  | "bedrock"
  | "vertex"
  | "cohere"
  | "voyage"
  | "mistral"
  | "groq"
  | "together"
  | "fireworks"
  | "perplexity"
  | "xai"
  | "deepseek"
  | "ollama"
  | "openai-compatible";

export type AICapability = "chat" | "embed";

export interface AIProvider {
  id: string;
  kind: AIProviderKind;
  name: string;
  model: string;
  base_url?: string;
  // Per-kind auth context — populated only when relevant.
  deployment?: string;  // azure-openai
  api_version?: string; // azure-openai
  region?: string;      // bedrock
  project?: string;     // vertex
  location?: string;    // vertex
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
  deployment?: string;
  api_version?: string;
  region?: string;
  project?: string;
  location?: string;
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
  gemini: [
    "gemini-2.0-flash",
    "gemini-2.0-pro",
    "gemini-1.5-flash",
    "gemini-1.5-pro",
    "text-embedding-004",
  ],
  "azure-openai": [], // deployment names are tenant-specific
  bedrock: [
    "anthropic.claude-3-5-sonnet-20241022-v2:0",
    "anthropic.claude-3-5-haiku-20241022-v1:0",
    "amazon.titan-text-express-v1",
    "amazon.titan-embed-text-v2:0",
    "meta.llama3-70b-instruct-v1:0",
    "mistral.mistral-large-2407-v1:0",
    "cohere.embed-english-v3",
  ],
  vertex: [
    "gemini-2.0-flash",
    "gemini-2.0-pro",
    "claude-3-5-sonnet-v2@20241022",
    "textembedding-gecko@003",
    "text-embedding-004",
  ],
  cohere: [
    "command-r-plus",
    "command-r",
    "embed-english-v3.0",
    "embed-multilingual-v3.0",
  ],
  voyage: ["voyage-3", "voyage-3-lite", "voyage-large-2", "voyage-code-3"],
  mistral: [
    "mistral-large-latest",
    "mistral-small-latest",
    "open-mistral-7b",
    "mistral-embed",
  ],
  groq: [
    "llama-3.3-70b-versatile",
    "llama-3.1-8b-instant",
    "mixtral-8x7b-32768",
  ],
  together: [
    "meta-llama/Llama-3.3-70B-Instruct-Turbo",
    "meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo",
    "mistralai/Mixtral-8x22B-Instruct-v0.1",
  ],
  fireworks: [
    "accounts/fireworks/models/llama-v3p3-70b-instruct",
    "accounts/fireworks/models/qwen2p5-72b-instruct",
  ],
  perplexity: [
    "llama-3.1-sonar-large-128k-online",
    "llama-3.1-sonar-small-128k-online",
  ],
  xai: ["grok-2-latest", "grok-2-mini"],
  deepseek: ["deepseek-chat", "deepseek-reasoner"],
  ollama: ["llama3.2", "qwen2.5", "nomic-embed-text"],
  "openai-compatible": [],
};
