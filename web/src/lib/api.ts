// Typed fetch client for the Plowered REST API.
//
// Auth: a bearer token is read from NEXT_PUBLIC_PLOWERED_TOKEN at build time
// or from sessionStorage at runtime. v0 only — production replaces this with
// a real OIDC flow.

import type { Asset, LineageResponse, SearchResponse, ApiError } from "./types";

const TOKEN_KEY = "plowered.token";

function token(): string {
  if (typeof window !== "undefined") {
    const stored = window.sessionStorage.getItem(TOKEN_KEY);
    if (stored) return stored;
  }
  return process.env.NEXT_PUBLIC_PLOWERED_TOKEN ?? "dev";
}

export function setToken(t: string) {
  if (typeof window !== "undefined") {
    window.sessionStorage.setItem(TOKEN_KEY, t);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${token()}`,
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const err: ApiError = await res.json().catch(() => ({
      code: "unknown",
      message: `HTTP ${res.status}`,
    }));
    throw new ApiCallError(err.code, err.message, res.status);
  }
  if (res.status === 204) return undefined as unknown as T;
  return (await res.json()) as T;
}

export class ApiCallError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

export const api = {
  listAssets: (params?: { type?: string; pageSize?: number }) => {
    const q = new URLSearchParams();
    if (params?.type) q.set("type", params.type);
    if (params?.pageSize) q.set("page_size", String(params.pageSize));
    const qs = q.toString();
    return request<{ assets: Asset[]; next_page_token: string }>(
      "GET",
      `/v1/assets${qs ? "?" + qs : ""}`,
    );
  },

  getAsset: (id: string) => request<Asset>("GET", `/v1/assets/${encodeURIComponent(id)}`),

  getAssetByQualifiedName: (qn: string) =>
    request<Asset>("GET", `/v1/assets:byQualifiedName?qn=${encodeURIComponent(qn)}`),

  search: (query: string, opts?: { limit?: number; type?: string }) =>
    request<SearchResponse>("POST", "/v1/assets:search", {
      query,
      limit: opts?.limit ?? 20,
      type: opts?.type,
    }),

  lineage: (id: string, direction: "upstream" | "downstream" = "upstream", depth = 1) =>
    request<LineageResponse>(
      "GET",
      `/v1/assets/${encodeURIComponent(id)}/lineage?direction=${direction}&depth=${depth}`,
    ),
};
