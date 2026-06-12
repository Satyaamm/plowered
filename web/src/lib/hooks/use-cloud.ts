"use client";

import { useQuery } from "@tanstack/react-query";
import { call } from "./_fetch";

// CloudBinding mirrors internal/api/http/cloud.go — one resolved
// infrastructure seam: backend kind + non-secret identifying detail.
export interface CloudBinding {
  kind: string;
  detail?: string;
}

export interface CloudStatus {
  object_store: CloudBinding;
  email: CloudBinding;
  database: CloudBinding;
  queue: CloudBinding;
  events: CloudBinding;
}

// useCloudStatus reports the platform's effective cloud bindings.
// Admin-gated server-side; non-admins get a 403 which surfaces as the
// hook's error state.
export function useCloudStatus() {
  return useQuery({
    queryKey: ["cloud-status"],
    queryFn: () => call<CloudStatus>("GET", "/v1/cloud/status"),
    staleTime: 5 * 60_000, // boot-time snapshot; it can't change without a restart
  });
}
