"use client";

import {
  MutationCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import {
  FluentProvider,
  SSRProvider,
  RendererProvider,
  createDOMRenderer,
} from "@fluentui/react-components";
import { useState } from "react";
import { ploweredLight } from "@/theme/fluent";
import { ToastBridge } from "@/components/toast-bridge";
import { MutationMeta, toast } from "@/lib/toast";

// Providers wraps the entire tree. Auth state is managed via the
// plowered_session cookie + /v1/auth/me query — no SessionProvider
// needed because we don't use next-auth.
export function Providers({ children }: { children: React.ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { staleTime: 30_000, retry: 1, refetchOnWindowFocus: false },
        },
        // Global toast on every mutation. Per-mutation overrides:
        //   meta: { successMessage: "…" }  → custom success copy
        //   meta: { errorMessage:   "…" }  → custom error copy
        //   meta: { silent: true }         → no toast at all
        // The fallback copy is intentionally generic so toasts never go
        // missing — adding nice copy is opt-in, opting out is explicit.
        mutationCache: new MutationCache({
          onSuccess: (_data, _vars, _ctx, mutation) => {
            const meta = (mutation.meta ?? {}) as MutationMeta;
            if (meta.silent) return;
            toast.success(meta.successMessage ?? "Saved");
          },
          onError: (err, _vars, _ctx, mutation) => {
            const meta = (mutation.meta ?? {}) as MutationMeta;
            if (meta.silent) return;
            const message =
              meta.errorMessage ??
              (err instanceof Error ? err.message : "Something went wrong");
            toast.error("Action failed", message);
          },
        }),
      }),
  );
  const [renderer] = useState(() => createDOMRenderer());

  return (
    <RendererProvider renderer={renderer}>
      <SSRProvider>
        <FluentProvider theme={ploweredLight}>
          <QueryClientProvider client={queryClient}>
            <ToastBridge />
            {children}
          </QueryClientProvider>
        </FluentProvider>
      </SSRProvider>
    </RendererProvider>
  );
}
