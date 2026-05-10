"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  FluentProvider,
  SSRProvider,
  RendererProvider,
  createDOMRenderer,
} from "@fluentui/react-components";
import { useState } from "react";
import { ploweredLight } from "@/theme/fluent";

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
      }),
  );
  const [renderer] = useState(() => createDOMRenderer());

  return (
    <RendererProvider renderer={renderer}>
      <SSRProvider>
        <FluentProvider theme={ploweredLight}>
          <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
        </FluentProvider>
      </SSRProvider>
    </RendererProvider>
  );
}
