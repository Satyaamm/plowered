"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

// Keep in lockstep with internal/core/feedback/feedback.go AllTypes etc.
export type FeedbackType = "bug" | "enhancement" | "question" | "praise";
export type FeedbackPriority = "low" | "normal" | "high" | "critical";
export type FeedbackStatus =
  | "new"
  | "triaged"
  | "in_progress"
  | "resolved"
  | "wont_fix";

export interface FeedbackItem {
  id: string;
  tenant_id: string;
  submitter_id: string;
  submitter_email?: string;
  type: FeedbackType;
  title: string;
  body?: string;
  page_url?: string;
  user_agent?: string;
  priority: FeedbackPriority;
  status: FeedbackStatus;
  assignee_id?: string;
  vote_count: number;
  voted_by_me?: boolean;
  created_at: string;
  updated_at: string;
}

export interface FeedbackComment {
  ID: string;
  ItemID: string;
  AuthorID: string;
  AuthorRole: string;
  Body: string;
  CreatedAt: string;
}

export interface SubmitFeedbackInput {
  type: FeedbackType;
  title: string;
  body?: string;
  priority?: FeedbackPriority;
  // Auto-captured client-side at submit time; consumers should not have
  // to set these manually unless they're submitting on behalf of a
  // different page (e.g. a triage tool).
  page_url?: string;
  user_agent?: string;
}

export interface FeedbackFilters {
  status?: FeedbackStatus | "";
  type?: FeedbackType | "";
  priority?: FeedbackPriority | "";
  cross_tenant?: boolean;
}

const KEY = ["feedback"];

export function useFeedbackList(filters: FeedbackFilters = {}) {
  return useQuery({
    queryKey: [...KEY, filters],
    queryFn: async () => {
      const qs = new URLSearchParams();
      if (filters.status) qs.set("status", filters.status);
      if (filters.type) qs.set("type", filters.type);
      if (filters.priority) qs.set("priority", filters.priority);
      if (filters.cross_tenant) qs.set("cross_tenant", "1");
      const path = `/v1/feedback${qs.toString() ? `?${qs.toString()}` : ""}`;
      const r = await call<{ items: FeedbackItem[] }>("GET", path);
      return r.items ?? [];
    },
  });
}

export function useFeedbackItem(id: string | null) {
  return useQuery({
    queryKey: [...KEY, "item", id],
    queryFn: () => call<FeedbackItem>("GET", `/v1/feedback/${id}`),
    enabled: !!id,
  });
}

export function useFeedbackComments(id: string | null) {
  return useQuery({
    queryKey: [...KEY, "comments", id],
    queryFn: async () => {
      const r = await call<{ comments: FeedbackComment[] }>(
        "GET",
        `/v1/feedback/${id}/comments`,
      );
      return r.comments ?? [];
    },
    enabled: !!id,
  });
}

export function useSubmitFeedback() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: SubmitFeedbackInput) =>
      call<FeedbackItem>("POST", "/v1/feedback", {
        ...body,
        // Auto-capture page URL + UA at submit time so the platform
        // admin gets repro context without the submitter needing to
        // paste either.
        page_url: body.page_url ?? (typeof window !== "undefined" ? window.location.href : ""),
        user_agent:
          body.user_agent ??
          (typeof navigator !== "undefined" ? navigator.userAgent : ""),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Thanks — feedback submitted." },
  });
}

export interface TriageFeedbackInput {
  type?: FeedbackType;
  title?: string;
  body?: string;
  priority?: FeedbackPriority;
  status?: FeedbackStatus;
  assignee_id?: string;
}

export function useTriageFeedback(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: TriageFeedbackInput) =>
      call<FeedbackItem>("PATCH", `/v1/feedback/${id}`, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Feedback updated" },
  });
}

export function useDeleteFeedback() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/feedback/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Feedback deleted" },
  });
}

export function useVoteFeedback() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, vote }: { id: string; vote: boolean }) =>
      call<{ vote_count: number }>(vote ? "POST" : "DELETE", `/v1/feedback/${id}/vote`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    // Silent — the row updates inline; a toast would be noise on every click.
    meta: { silent: true },
  });
}

export function useCommentFeedback(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: string) =>
      call<FeedbackComment>("POST", `/v1/feedback/${id}/comments`, { body }),
    onSuccess: () => qc.invalidateQueries({ queryKey: [...KEY, "comments", id] }),
    meta: { successMessage: "Comment posted" },
  });
}
