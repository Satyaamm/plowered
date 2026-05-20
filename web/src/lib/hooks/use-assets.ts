"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { call } from "./_fetch";

// AssetColumn is the slim shape every column-picker UI needs. Driven
// by the lineage:defines walk the Schema tab already uses.
export interface AssetColumn {
  id: string;
  name: string;
  data_type: string;
  ordinal: number;
}

// useAssetColumns returns the columns of a table/view asset, sorted
// by ordinal position. Powers column pickers across check designer
// and contract editor. Disabled when assetId is blank.
export function useAssetColumns(assetId: string | null) {
  return useQuery({
    queryKey: ["asset-columns", assetId],
    enabled: !!assetId,
    queryFn: async () => {
      const r = await api.children(assetId as string);
      const neighbors = ((r as any).neighbors ?? []) as Array<{
        id: string;
        name: string;
        type: string;
        properties?: Record<string, any>;
      }>;
      return neighbors
        .filter((n) => n.type === "column")
        .map<AssetColumn>((n) => ({
          id: n.id,
          name: n.name,
          data_type: String(n.properties?.data_type ?? ""),
          ordinal: Number(n.properties?.ordinal_pos ?? 0),
        }))
        .sort((a, b) => a.ordinal - b.ordinal);
    },
  });
}

// Asset is a minimal shape covering the fields these hooks touch. The
// full Asset type lives in lib/types.ts — kept narrow here so the
// hook surface doesn't carry the entire catalog model.
interface AssetPatch {
  description?: string;
  description_ai?: string;
  trust?: string;
}

// useDescribeAsset asks the backend to generate an AI suggestion for
// an asset's description. Silent — the calling component opens a
// dialog with the result, so a toast would duplicate the feedback.
export interface DescribeSuggestion {
  asset_id: string;
  suggestion: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  generated_at: string;
}

export function useDescribeAsset(assetId: string) {
  return useMutation({
    mutationFn: () =>
      call<DescribeSuggestion>("POST", `/v1/assets/${assetId}/describe:ai`),
    meta: { silent: true },
  });
}

// useUpdateAsset patches arbitrary fields on an asset. Used by the
// describe-suggestion dialog's Save button to write the accepted text
// into asset.description_ai (kept separate from the user-edited
// asset.description so the auto-suggestion is visibly distinguishable
// in the UI / future audit).
export function useUpdateAsset(assetId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: AssetPatch) =>
      call<unknown>("PATCH", `/v1/assets/${assetId}`, patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["asset", assetId] });
    },
    meta: { successMessage: "Description saved" },
  });
}

// useUpdateAssetOwners is the focused partial-update for the owners
// field. Server-side merge (no fetch-then-PATCH round-trip from the
// client) keeps the operation atomic.
export function useUpdateAssetOwners(assetId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (owners: string[]) =>
      call<unknown>("PATCH", `/v1/assets/${assetId}/owners`, { owners }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["asset", assetId] });
    },
    meta: { successMessage: "Owners updated" },
  });
}
