"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

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
