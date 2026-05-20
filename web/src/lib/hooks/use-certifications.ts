"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export type CertificationStatus =
  | "proposed"
  | "certified"
  | "rejected"
  | "revoked";

export interface Certification {
  id: string;
  tenant_id: string;
  asset_id: string;
  status: CertificationStatus;
  proposed_by?: string;
  proposed_at: string;
  reviewed_by?: string;
  reviewed_at?: string;
  justification?: string;
  review_note?: string;
}

interface AssetCertificationsResponse {
  latest: Certification | null;
  history: Certification[];
}

export function useAssetCertifications(assetId: string | null) {
  return useQuery({
    queryKey: ["asset-certifications", assetId],
    enabled: !!assetId,
    queryFn: () =>
      call<AssetCertificationsResponse>(
        "GET",
        `/v1/assets/${assetId}/certifications`,
      ),
  });
}

export function usePendingCertifications() {
  return useQuery({
    queryKey: ["certifications-pending"],
    queryFn: async () => {
      const d = await call<{ pending: Certification[] }>(
        "GET",
        "/v1/certifications/pending",
      );
      return d.pending ?? [];
    },
  });
}

export function useProposeCertification(assetId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (justification: string) =>
      call<Certification>(
        "POST",
        `/v1/assets/${assetId}/certifications`,
        { justification },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["asset-certifications", assetId] });
      qc.invalidateQueries({ queryKey: ["certifications-pending"] });
    },
    meta: { successMessage: "Certification proposed" },
  });
}

export function useApproveCertification() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; note: string }) =>
      call<Certification>(
        "POST",
        `/v1/certifications/${input.id}/approve`,
        { note: input.note },
      ),
    onSuccess: (cert) => {
      qc.invalidateQueries({ queryKey: ["asset-certifications", cert.asset_id] });
      qc.invalidateQueries({ queryKey: ["certifications-pending"] });
    },
    meta: { successMessage: "Certification approved" },
  });
}

export function useRejectCertification() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { id: string; note: string }) =>
      call<Certification>(
        "POST",
        `/v1/certifications/${input.id}/reject`,
        { note: input.note },
      ),
    onSuccess: (cert) => {
      qc.invalidateQueries({ queryKey: ["asset-certifications", cert.asset_id] });
      qc.invalidateQueries({ queryKey: ["certifications-pending"] });
    },
    meta: { successMessage: "Certification rejected" },
  });
}

export function useRevokeCertification(assetId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (note: string) =>
      call<Certification>(
        "POST",
        `/v1/assets/${assetId}/certifications/revoke`,
        { note },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["asset-certifications", assetId] });
    },
    meta: { successMessage: "Certification revoked" },
  });
}
