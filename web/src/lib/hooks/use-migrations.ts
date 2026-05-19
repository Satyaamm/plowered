"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { call } from "./_fetch";

export type MigrationMode = "snapshot" | "incremental" | "cdc";
export type MigrationWriteMode = "truncate_and_replace" | "append";
export type MigrationRunStatus = "running" | "succeeded" | "failed";

export interface MigrationColumnMap {
  source_col: string;
  dest_col: string;
}

export interface MigrationPlan {
  id: string;
  tenant_id: string;
  name: string;
  source_connection_id: string;
  source_schema: string;
  source_table: string;
  dest_connection_id: string;
  dest_schema: string;
  dest_table: string;
  column_map: MigrationColumnMap[];
  mode: MigrationMode;
  write_mode: MigrationWriteMode;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface MigrationRun {
  id: string;
  tenant_id: string;
  plan_id: string;
  status: MigrationRunStatus;
  started_at: string;
  finished_at?: string;
  rows_read: number;
  rows_written: number;
  checkpoint_uri?: string;
  error?: string;
}

const KEY = ["migrations"];

export function useMigrationPlans() {
  return useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const d = await call<{ plans: MigrationPlan[] }>("GET", "/v1/migrations");
      return d.plans ?? [];
    },
  });
}

export function useMigrationPlan(id: string | null) {
  return useQuery({
    queryKey: ["migration-plan", id],
    enabled: !!id,
    queryFn: () => call<MigrationPlan>("GET", `/v1/migrations/${id}`),
  });
}

export function useMigrationRuns(planId: string | null) {
  return useQuery({
    queryKey: ["migration-runs", planId],
    enabled: !!planId,
    queryFn: async () => {
      const d = await call<{ runs: MigrationRun[] }>(
        "GET",
        `/v1/migrations/${planId}/runs`,
      );
      return d.runs ?? [];
    },
    // Poll while runs may be in flight. 5s is enough to feel live.
    refetchInterval: (q) => {
      const data = q.state.data as MigrationRun[] | undefined;
      if (!data) return 5000;
      return data.some((r) => r.status === "running") ? 5000 : false;
    },
  });
}

export interface CreateMigrationPlanInput {
  name: string;
  source_connection_id: string;
  source_schema: string;
  source_table: string;
  dest_connection_id: string;
  dest_schema: string;
  dest_table: string;
  column_map: MigrationColumnMap[];
  mode: MigrationMode;
  write_mode: MigrationWriteMode;
}

export function useCreateMigrationPlan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: CreateMigrationPlanInput) =>
      call<MigrationPlan>("POST", "/v1/migrations", p),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Migration plan created" },
  });
}

export function useDeleteMigrationPlan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => call<void>("DELETE", `/v1/migrations/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
    meta: { successMessage: "Migration plan deleted" },
  });
}

export function useRunMigrationPlan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      call<MigrationRun>("POST", `/v1/migrations/${id}/run`),
    onSuccess: (_run, id) => {
      qc.invalidateQueries({ queryKey: ["migration-runs", id] });
    },
    // Silent — the run-history card on the page is the feedback. Toast
    // would duplicate the visible status badge.
    meta: { silent: true },
  });
}
