"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

// Type returned by GET /v1/auth/me. Mirrors the Go meResp struct.
export interface Me {
  user_id: string;
  tenant_id: string;
  tenant_name: string;
  tenant_slug: string;
  email: string;
  full_name: string;
  roles: string[];
  email_verified: boolean;
  tour_completed: boolean;
}

// One row from GET /v1/workspaces/mine.
export interface Workspace {
  id: string;
  name: string;
  slug: string;
  roles: string[];
}

const ME_KEY = ["auth", "me"];

// Every fetch in this file goes to /api/v1/auth/* — Next.js rewrites
// that to the backend. credentials: "include" forces the browser to
// send/receive the plowered_session cookie even on cross-origin dev
// setups.
async function call<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: "include",
  });
  if (!res.ok) {
    let payload: any = {};
    try {
      payload = await res.json();
    } catch {
      // body was empty or non-JSON
    }
    const err = new Error(payload.message ?? `HTTP ${res.status}`) as Error & {
      code?: string;
      status?: number;
      payload?: any;
    };
    err.code = payload.code;
    err.status = res.status;
    err.payload = payload;
    throw err;
  }
  if (res.status === 204) return undefined as unknown as T;
  return (await res.json()) as T;
}

// useMe fetches the current session principal. Returns null when not
// signed in (401). Components like RequireAuth use the returned `error`
// to redirect to /login.
export function useMe() {
  return useQuery({
    queryKey: ME_KEY,
    queryFn: () => call<Me>("GET", "/v1/auth/me"),
    retry: false,
    staleTime: 30_000,
  });
}

// useMyWorkspaces lists every tenant the authenticated user belongs to.
// Powers the topbar workspace switcher.
export function useMyWorkspaces() {
  return useQuery({
    queryKey: ["workspaces", "mine"],
    queryFn: async () => {
      const d = await call<{ workspaces: Workspace[] }>("GET", "/v1/workspaces/mine");
      return d.workspaces ?? [];
    },
    staleTime: 60_000,
  });
}

export function useCompleteTour() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      call<{ tour_completed: boolean }>("POST", "/v1/account/tour:complete"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ME_KEY }),
  });
}

export function useResetTour() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () =>
      call<{ tour_completed: boolean }>("POST", "/v1/account/tour:reset"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ME_KEY }),
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (args: { email: string; password: string }) =>
      call<Me>("POST", "/v1/auth/login", args),
    onSuccess: (me) => {
      qc.setQueryData(ME_KEY, me);
    },
  });
}

export function useSignup() {
  return useMutation({
    mutationFn: (args: {
      email: string;
      password: string;
      confirm_password?: string;
      first_name?: string;
      last_name?: string;
      full_name?: string;
      phone?: string;
      phone_country?: string;
      workspace_name: string;
      workspace_slug?: string;
      accept_terms?: boolean;
    }) =>
      call<{ tenant_id: string; user_id: string; status: string; message: string }>(
        "POST",
        "/v1/auth/signup",
        args,
      ),
  });
}

export function useResendVerification() {
  return useMutation({
    mutationFn: (email: string) =>
      call<{ status: string }>("POST", "/v1/auth/resend-verification", { email }),
  });
}

export function useForgotPassword() {
  return useMutation({
    mutationFn: (email: string) =>
      call<{ status: string; message: string }>(
        "POST",
        "/v1/auth/forgot-password",
        { email },
      ),
  });
}

export function useResetPassword() {
  return useMutation({
    mutationFn: (args: { token: string; password: string }) =>
      call<{ status: string }>("POST", "/v1/auth/reset-password", args),
  });
}

export function useVerifyEmail() {
  return useMutation({
    mutationFn: (token: string) =>
      call<{ status: string; message: string }>(
        "GET",
        `/v1/auth/verify?token=${encodeURIComponent(token)}`,
      ),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => call<void>("POST", "/v1/auth/logout"),
    onSuccess: () => {
      qc.setQueryData(ME_KEY, null);
      qc.removeQueries({ queryKey: ME_KEY });
    },
  });
}
