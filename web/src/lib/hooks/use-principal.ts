"use client";

import { useMe } from "@/lib/auth-client";

// usePrincipal exposes the authenticated user as a friendly shape for
// pages and the topbar. Backed by GET /v1/auth/me; the cookie session
// rides on the Next.js rewrite so credentials are automatic.
export function usePrincipal() {
  const { data, error, isLoading } = useMe();
  if (data) {
    return {
      principal: {
        id: data.user_id,
        tenantId: data.tenant_id,
        email: data.email,
        fullName: data.full_name,
        roles: data.roles ?? [],
        verified: data.email_verified,
      },
      loading: false,
      authenticated: true as const,
    };
  }
  return {
    principal: null,
    loading: isLoading,
    authenticated: false as const,
    error: error ?? null,
  };
}
