// Mirrors internal/core/graph/types.go. Keep in sync until proto codegen
// produces TypeScript bindings via Buf.

export type AssetType =
  | "database"
  | "schema"
  | "table"
  | "view"
  | "column"
  | "dashboard"
  | "report"
  | "dbt_model"
  | "dag"
  | "ml_model"
  | "glossary_term"
  | "";

export type TrustLevel = "unverified" | "draft" | "reviewed" | "certified" | "deprecated";

export interface Asset {
  id: string;
  tenant_id: string;
  qualified_name: string;
  type: AssetType;
  name: string;
  description?: string;
  description_ai?: string;
  trust: TrustLevel;
  tags?: string[];
  owners?: string[];
  properties?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  created_by?: string;
  updated_by?: string;
}

export interface SearchHit {
  asset: Asset;
  score: number;
}

export interface SearchResponse {
  hits: SearchHit[];
}

export interface LineageEdgeView {
  id: string;
  source_id: string;
  target_id: string;
  kind: string;
}

export interface LineageResponse {
  root: Asset;
  direction: "upstream" | "downstream";
  depth: number;
  edges: LineageEdgeView[];
  truncated?: boolean;
}

export interface ApiError {
  code: string;
  message: string;
}
