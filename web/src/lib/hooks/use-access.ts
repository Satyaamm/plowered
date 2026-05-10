"use client";

import { useMutation } from "@tanstack/react-query";
import { call } from "./_fetch";

export interface AccessRow {
  asset_id: string;
  qualified_name: string;
  type: string;
  tags?: string[];
  reason: string;
}

export interface AccessPreview {
  principal: {
    user_id?: string;
    email?: string;
    roles: string[];
    groups?: string[];
    tenant_id: string;
  };
  verb: string;
  total: number;
  visible: AccessRow[];
  denied: AccessRow[];
}

export interface AccessRequest {
  role?: string;
  groups?: string[];
  email?: string;
  verb?: string;
  limit?: number;
}

/**
 * useAccessPreview answers "as user X (or principal {role,groups}), what
 * is visible in the catalog?". The endpoint walks every asset and runs
 * the policy engine, returning two slices for the UI to render.
 */
export function useAccessPreview() {
  return useMutation({
    mutationFn: (req: AccessRequest) =>
      call<AccessPreview>("POST", "/v1/access/preview", req),
  });
}
