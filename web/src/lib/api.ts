// Typed fetch client for the Plowered REST API.
//
// Auth: the session rides on the plowered_session HttpOnly cookie set by
// POST /v1/auth/login. We pass `credentials: "include"` so the browser
// always sends + receives that cookie even when the dev origin differs
// from the API. Bearer-token / API-key paths are intentionally absent
// from this client — service-to-service code uses a different SDK.

import type { Asset, LineageResponse, SearchResponse, ApiError } from "./types";
import type {
  Pipeline,
  Run,
  TaskRun,
  Check,
  CheckRun,
  Channel,
  NotifyRule,
  Delivery,
  PolicyRule,
} from "./types-orchestration";

export interface RequestOptions {
  /** AbortSignal for cancellation (TanStack Query supplies this). */
  signal?: AbortSignal;
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  opts?: RequestOptions,
): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    signal: opts?.signal,
    credentials: "include",
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

// ---------- catalog ----------

export const catalogApi = {
  listAssets: (params?: { type?: string; pageSize?: number }, opts?: RequestOptions) => {
    const q = new URLSearchParams();
    if (params?.type) q.set("type", params.type);
    if (params?.pageSize) q.set("page_size", String(params.pageSize));
    const qs = q.toString();
    return request<{ assets: Asset[]; next_page_token: string }>(
      "GET",
      `/v1/assets${qs ? "?" + qs : ""}`,
      undefined,
      opts,
    );
  },

  getAsset: (id: string, opts?: RequestOptions) =>
    request<Asset>("GET", `/v1/assets/${encodeURIComponent(id)}`, undefined, opts),

  getAssetByQualifiedName: (qn: string, opts?: RequestOptions) =>
    request<Asset>(
      "GET",
      `/v1/assets:byQualifiedName?qn=${encodeURIComponent(qn)}`,
      undefined,
      opts,
    ),

  search: (
    query: string,
    params?: { limit?: number; type?: string },
    opts?: RequestOptions,
  ) =>
    request<SearchResponse>(
      "POST",
      "/v1/assets:search",
      { query, limit: params?.limit ?? 20, type: params?.type },
      opts,
    ),

  lineage: (
    id: string,
    direction: "upstream" | "downstream" | "both" = "both",
    depth = 1,
    opts?: RequestOptions,
  ) =>
    request<LineageResponse>(
      "GET",
      `/v1/assets/${encodeURIComponent(id)}/lineage?direction=${direction}&depth=${depth}`,
      undefined,
      opts,
    ),

  // children walks `defines` edges downstream from this asset. Used by
  // the Schema tab to enumerate columns of a table / tables of a schema.
  children: (id: string, opts?: RequestOptions) =>
    request<LineageResponse>(
      "GET",
      `/v1/assets/${encodeURIComponent(id)}/lineage?direction=downstream&depth=1&kind=defines`,
      undefined,
      opts,
    ),
};

// Back-compat alias — existing pages import { api }.
export const api = catalogApi;

// ---------- pipelines / runs ----------

export const pipelinesApi = {
  list: (opts?: RequestOptions) =>
    request<{ pipelines: Pipeline[] }>("GET", "/v1/pipelines", undefined, opts),
  get: (id: string, opts?: RequestOptions) =>
    request<Pipeline>("GET", `/v1/pipelines/${encodeURIComponent(id)}`, undefined, opts),
  create: (p: Partial<Pipeline>, opts?: RequestOptions) =>
    request<Pipeline>("POST", "/v1/pipelines", p, opts),
  update: (id: string, p: Partial<Pipeline>, opts?: RequestOptions) =>
    request<Pipeline>("PATCH", `/v1/pipelines/${encodeURIComponent(id)}`, p, opts),
  remove: (id: string, opts?: RequestOptions) =>
    request<void>("DELETE", `/v1/pipelines/${encodeURIComponent(id)}`, undefined, opts),
  trigger: (id: string, opts?: RequestOptions) =>
    request<Run>("POST", `/v1/pipelines/${encodeURIComponent(id)}/trigger`, {}, opts),
};

export const runsApi = {
  list: (params?: { pipelineId?: string; limit?: number }, opts?: RequestOptions) => {
    const q = new URLSearchParams();
    if (params?.pipelineId) q.set("pipeline_id", params.pipelineId);
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return request<{ runs: Run[] }>("GET", `/v1/runs${qs ? "?" + qs : ""}`, undefined, opts);
  },
  get: (id: string, opts?: RequestOptions) =>
    request<Run>("GET", `/v1/runs/${encodeURIComponent(id)}`, undefined, opts),
  tasks: (id: string, opts?: RequestOptions) =>
    request<{ task_runs: TaskRun[] }>(
      "GET",
      `/v1/runs/${encodeURIComponent(id)}/tasks`,
      undefined,
      opts,
    ),
};

// ---------- quality ----------

export const checksApi = {
  list: (params?: { assetId?: string }, opts?: RequestOptions) => {
    const q = new URLSearchParams();
    if (params?.assetId) q.set("asset_id", params.assetId);
    const qs = q.toString();
    return request<{ checks: Check[] }>(
      "GET",
      `/v1/checks${qs ? "?" + qs : ""}`,
      undefined,
      opts,
    );
  },
  get: (id: string, opts?: RequestOptions) =>
    request<Check>("GET", `/v1/checks/${encodeURIComponent(id)}`, undefined, opts),
  create: (c: Partial<Check>, opts?: RequestOptions) =>
    request<Check>("POST", "/v1/checks", c, opts),
  update: (id: string, c: Partial<Check>, opts?: RequestOptions) =>
    request<Check>("PATCH", `/v1/checks/${encodeURIComponent(id)}`, c, opts),
  remove: (id: string, opts?: RequestOptions) =>
    request<void>("DELETE", `/v1/checks/${encodeURIComponent(id)}`, undefined, opts),
  runs: (id: string, params?: { limit?: number }, opts?: RequestOptions) => {
    const q = new URLSearchParams();
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return request<{ runs: CheckRun[] }>(
      "GET",
      `/v1/checks/${encodeURIComponent(id)}/runs${qs ? "?" + qs : ""}`,
      undefined,
      opts,
    );
  },
  trigger: (
    id: string,
    body?: { source_id?: string; sample_percent?: number; timeout_sec?: number },
    opts?: RequestOptions,
  ) =>
    request<{ status: string; check_id: string }>(
      "POST",
      `/v1/checks/${encodeURIComponent(id)}/run`,
      body ?? {},
      opts,
    ),
};

// ---------- notifications ----------

export const notificationsApi = {
  channels: {
    list: (opts?: RequestOptions) =>
      request<{ channels: Channel[] }>(
        "GET",
        "/v1/notifications/channels",
        undefined,
        opts,
      ),
    create: (c: Partial<Channel>, opts?: RequestOptions) =>
      request<Channel>("POST", "/v1/notifications/channels", c, opts),
  },
  rules: {
    list: (opts?: RequestOptions) =>
      request<{ rules: NotifyRule[] }>("GET", "/v1/notifications/rules", undefined, opts),
    create: (r: Partial<NotifyRule>, opts?: RequestOptions) =>
      request<NotifyRule>("POST", "/v1/notifications/rules", r, opts),
  },
  deliveries: (params?: { limit?: number }, opts?: RequestOptions) => {
    const q = new URLSearchParams();
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return request<{ deliveries: Delivery[] }>(
      "GET",
      `/v1/notifications/deliveries${qs ? "?" + qs : ""}`,
      undefined,
      opts,
    );
  },
};

// ---------- policies ----------

export const policiesApi = {
  list: (opts?: RequestOptions) =>
    request<{ rules: PolicyRule[] }>("GET", "/v1/policies", undefined, opts),
  create: (r: Partial<PolicyRule>, opts?: RequestOptions) =>
    request<PolicyRule>("POST", "/v1/policies", r, opts),
  remove: (id: string, opts?: RequestOptions) =>
    request<void>("DELETE", `/v1/policies/${encodeURIComponent(id)}`, undefined, opts),
};

// ---------- glossary ----------

export interface GlossaryTerm {
  id: string;
  name: string;
  definition: string;
  parent_id?: string;
  status: "draft" | "approved" | "deprecated";
  owner_id?: string;
  created_at: string;
  updated_at: string;
}

export interface AssetTerm {
  term_id: string;
  name: string;
  definition: string;
  status: string;
  asset_id: string;
  assigned_at: string;
}

export const glossaryApi = {
  list: (opts?: RequestOptions) =>
    request<{ terms: GlossaryTerm[] }>("GET", "/v1/glossary/terms", undefined, opts),
  get: (id: string, opts?: RequestOptions) =>
    request<GlossaryTerm>("GET", `/v1/glossary/terms/${encodeURIComponent(id)}`, undefined, opts),
  create: (t: Partial<GlossaryTerm>, opts?: RequestOptions) =>
    request<GlossaryTerm>("POST", "/v1/glossary/terms", t, opts),
  update: (id: string, t: Partial<GlossaryTerm>, opts?: RequestOptions) =>
    request<GlossaryTerm>("PATCH", `/v1/glossary/terms/${encodeURIComponent(id)}`, t, opts),
  remove: (id: string, opts?: RequestOptions) =>
    request<void>("DELETE", `/v1/glossary/terms/${encodeURIComponent(id)}`, undefined, opts),

  assign: (id: string, assetId: string, opts?: RequestOptions) =>
    request<void>(
      "POST",
      `/v1/glossary/terms/${encodeURIComponent(id)}/assignments`,
      { asset_id: assetId },
      opts,
    ),
  unassign: (id: string, assetId: string, opts?: RequestOptions) =>
    request<void>(
      "DELETE",
      `/v1/glossary/terms/${encodeURIComponent(id)}/assignments/${encodeURIComponent(assetId)}`,
      undefined,
      opts,
    ),
  assetsForTerm: (id: string, opts?: RequestOptions) =>
    request<{ asset_ids: string[] }>(
      "GET",
      `/v1/glossary/terms/${encodeURIComponent(id)}/assets`,
      undefined,
      opts,
    ),
  termsForAsset: (assetId: string, opts?: RequestOptions) =>
    request<{ terms: AssetTerm[] }>(
      "GET",
      `/v1/assets/${encodeURIComponent(assetId)}/terms`,
      undefined,
      opts,
    ),
};
