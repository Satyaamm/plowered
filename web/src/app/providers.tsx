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
import { MutationMeta, deriveErrorTitle, toast } from "@/lib/toast";
import { ApiError } from "@/lib/hooks/_fetch";

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
        //   meta: { successMessage: "…" }  → custom success title
        //   meta: { errorTitle:     "…" }  → custom error title
        //   meta: { errorMessage:   "…" }  → custom error body
        //   meta: { silent: true }         → no toast at all
        //
        // Success fallback intentionally generic so toasts never go
        // missing — every hook adds nice copy via successMessage; the
        // "Saved" fallback exists only as a safety net.
        //
        // Error fallback is smart: ApiError carries the structured
        // error code from the backend (validation_failed / forbidden /
        // rate_limited / …) which deriveErrorTitle() maps to a friendly
        // toast title. The error.message becomes the body.
        mutationCache: new MutationCache({
          onSuccess: (_data, _vars, _ctx, mutation) => {
            const meta = (mutation.meta ?? {}) as MutationMeta;
            if (meta.silent) return;
            toast.success(meta.successMessage ?? "Saved");
          },
          onError: (err, _vars, _ctx, mutation) => {
            const meta = (mutation.meta ?? {}) as MutationMeta;
            if (meta.silent) return;

            let title: string;
            let body: string;

            if (err instanceof ApiError) {
              title = meta.errorTitle ?? deriveErrorTitle(err.code, err.status);
              body = meta.errorMessage ?? err.message;
            } else if (err instanceof Error) {
              title = meta.errorTitle ?? "Network error";
              body = meta.errorMessage ?? err.message;
            } else {
              title = meta.errorTitle ?? "Action failed";
              body = meta.errorMessage ?? "Something went wrong";
            }
            toast.error(title, body);
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
