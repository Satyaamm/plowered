"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FluentProvider, SSRProvider, RendererProvider, createDOMRenderer } from "@fluentui/react-components";
import { useState } from "react";
import { ploweredLight } from "@/theme";

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
