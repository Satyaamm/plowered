"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { notificationsApi } from "@/lib/api";
import type { Channel, NotifyRule } from "@/lib/types-orchestration";

export function useChannels() {
  return useQuery({
    queryKey: ["notify-channels"],
    queryFn: ({ signal }) => notificationsApi.channels.list({ signal }),
    select: (d) => d.channels ?? [],
  });
}

export function useCreateChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (c: Partial<Channel>) => notificationsApi.channels.create(c),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notify-channels"] }),
  });
}

export function useNotifyRules() {
  return useQuery({
    queryKey: ["notify-rules"],
    queryFn: ({ signal }) => notificationsApi.rules.list({ signal }),
    select: (d) => d.rules ?? [],
  });
}

export function useCreateNotifyRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (r: Partial<NotifyRule>) => notificationsApi.rules.create(r),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notify-rules"] }),
  });
}

export function useDeliveries(limit = 100) {
  return useQuery({
    queryKey: ["notify-deliveries", limit],
    queryFn: ({ signal }) => notificationsApi.deliveries({ limit }, { signal }),
    select: (d) => d.deliveries ?? [],
    refetchInterval: 10_000,
  });
}
