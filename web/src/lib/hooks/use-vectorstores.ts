"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

// Keep in lockstep with internal/core/vectorstore/vectorstore.go AllKinds.
export type VectorStoreKind =
  | "pgvector"
  | "memory"
  | "pinecone"
  | "weaviate"
  | "qdrant";

export interface VectorStore {
  id: string;
  kind: VectorStoreKind;
  name: string;
  endpoint?: string;
  index_name?: string;
  class_name?: string;
  collection?: string;
  dimension?: number;
  is_primary: boolean;
  last_tested_at?: string;
  last_test_ok: boolean;
  last_test_error?: string;
  created_at: string;
  updated_at: string;
}

export interface VectorStoreInput {
  kind: VectorStoreKind;
  name: string;
  endpoint?: string;
  index_name?: string;
  class_name?: string;
  collection?: string;
  dimension?: number;
  api_key?: string;
  is_primary?: boolean;
}

const KEY = ["vectorstores"];

export function useVectorStores() {
  return useQuery({
    queryKey: KEY,
    queryFn: () =>
      call<{ vectorstores: VectorStore[] }>("GET", "/v1/vectorstores").then(
        (r) => r.vectorstores ?? [],
      ),
  });
}

export function useCreateVectorStore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: VectorStoreInput) =>
      call<VectorStore>("POST", "/v1/vectorstores", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Vector store added" },
  });
}

export function useDeleteVectorStore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/vectorstores/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Vector store removed" },
  });
}

export function useTestStoredVectorStore() {
  return useMutation({
    mutationFn: (id: string) =>
      call<{ ok: boolean; error?: string }>(
        "POST",
        `/v1/vectorstores/${id}/test`,
      ),
    meta: { silent: true },
  });
}

export function useTestInlineVectorStore() {
  return useMutation({
    mutationFn: (body: VectorStoreInput) =>
      call<{ ok: boolean; error?: string }>(
        "POST",
        "/v1/vectorstores:test",
        body,
      ),
    meta: { silent: true },
  });
}

export function useSetPrimaryVectorStore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<void>("POST", `/v1/vectorstores/${id}/primary`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Primary vector store updated" },
  });
}
