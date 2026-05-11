"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export interface AccountSession {
  id: string;
  ip?: string;
  user_agent?: string;
  issued_at: string;
  last_seen_at: string;
  expires_at: string;
  current: boolean;
}

const SESSIONS_KEY = ["account", "sessions"];

export function useUpdateProfile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      first_name?: string;
      last_name?: string;
      phone?: string;
      phone_country?: string;
    }) => call<{ status: string }>("PATCH", "/v1/account/profile", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["auth", "me"] });
    },
  });
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (body: { current_password: string; new_password: string }) =>
      call<{ status: string; message: string }>(
        "POST",
        "/v1/account/change-password",
        body,
      ),
  });
}

export function useAccountSessions() {
  return useQuery({
    queryKey: SESSIONS_KEY,
    queryFn: () =>
      call<{ sessions: AccountSession[] }>("GET", "/v1/account/sessions").then(
        (r) => r.sessions ?? [],
      ),
  });
}

export function useRevokeSession() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<void>("DELETE", `/v1/account/sessions/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: SESSIONS_KEY }),
  });
}

export function useSignOutEverywhere() {
  return useMutation({
    mutationFn: () => call<void>("DELETE", "/v1/account/sessions"),
  });
}
