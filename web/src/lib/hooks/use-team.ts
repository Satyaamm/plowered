"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export type MemberRole = "viewer" | "editor" | "steward" | "admin";

export interface Member {
  user_id: string;
  email: string;
  full_name: string;
  first_name?: string;
  last_name?: string;
  roles: string[];
  status: string;
  invited_at: string;
  joined_at?: string;
}

export interface Invite {
  id: string;
  email: string;
  roles: string[];
  status: "pending" | "accepted" | "revoked" | "expired";
  invited_by?: string;
  expires_at: string;
  created_at: string;
  accepted_at?: string;
  revoked_at?: string;
}

const MEMBERS_KEY = ["team", "members"];
const INVITES_KEY = ["team", "invites"];

export function useMembers() {
  return useQuery({
    queryKey: MEMBERS_KEY,
    queryFn: () =>
      call<{ members: Member[] }>("GET", "/v1/members").then(
        (r) => r.members ?? [],
      ),
  });
}

export function useInvites(includeAll = false) {
  return useQuery({
    queryKey: [...INVITES_KEY, { includeAll }],
    queryFn: () =>
      call<{ invites: Invite[] }>(
        "GET",
        includeAll ? "/v1/invites?include=all" : "/v1/invites",
      ).then((r) => r.invites ?? []),
  });
}

export function useCreateInvite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { email: string; roles: string[] }) =>
      call<Invite>("POST", "/v1/invites", body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: INVITES_KEY });
    },
    meta: { successMessage: "Invite sent" },
  });
}

export function useRevokeInvite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/invites/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: INVITES_KEY });
    },
    meta: { successMessage: "Invite revoked" },
  });
}

export function useUpdateMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, roles }: { userId: string; roles: string[] }) =>
      call<{ status: string }>("PATCH", `/v1/members/${userId}`, { roles }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: MEMBERS_KEY });
    },
    meta: { successMessage: "Member updated" },
  });
}

export function useRemoveMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) =>
      call<void>("DELETE", `/v1/members/${userId}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: MEMBERS_KEY });
    },
    meta: { successMessage: "Member removed" },
  });
}

// ----- public accept-invite flow -----

export interface InviteInfo {
  email: string;
  workspace_name: string;
  roles: string[];
}

export function useInviteInfo(token: string | null) {
  return useQuery({
    queryKey: ["invite-info", token],
    enabled: !!token,
    retry: false,
    queryFn: () =>
      call<InviteInfo>(
        "GET",
        `/v1/auth/invite-info?token=${encodeURIComponent(token!)}`,
      ),
  });
}

export function useAcceptInvite() {
  return useMutation({
    mutationFn: (body: {
      token: string;
      password: string;
      first_name?: string;
      last_name?: string;
    }) =>
      call<{ status: string; tenant_id: string; user_id: string }>(
        "POST",
        "/v1/auth/accept-invite",
        body,
      ),
    meta: { successMessage: "Welcome to the workspace" },
  });
}

export const ROLE_OPTIONS: { value: MemberRole; label: string; hint: string }[] =
  [
    {
      value: "viewer",
      label: "Viewer",
      hint: "Read-only across the catalog.",
    },
    {
      value: "editor",
      label: "Editor",
      hint: "Edit assets, run pipelines, manage checks.",
    },
    {
      value: "steward",
      label: "Steward",
      hint: "Approve glossary, manage tags + classifications.",
    },
    {
      value: "admin",
      label: "Admin",
      hint: "Manage team, billing, policies and connections.",
    },
  ];
