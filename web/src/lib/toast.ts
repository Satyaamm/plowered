// Global toast adapter.
//
// Why a module-level controller instead of just exposing a hook:
// React Query's MutationCache callbacks fire OUTSIDE React's render tree
// (they live on the QueryClient instance), so they can't call hooks. We
// register the Fluent useToastController dispatcher with this module on
// mount, then both React components (via the exported helpers) and the
// MutationCache (via the same helpers) can fire toasts uniformly.

import type { ToastIntent } from "@fluentui/react-components";

export const TOASTER_ID = "app";

export interface MutationMeta {
  /** When true, no toast fires for this mutation regardless of outcome. */
  silent?: boolean;
  /** Override the default success copy. */
  successMessage?: string;
  /** Override the default error copy (otherwise we surface error.message). */
  errorMessage?: string;
}

type Dispatcher = (args: {
  title: string;
  body?: string;
  intent: ToastIntent;
}) => void;

let dispatcher: Dispatcher | null = null;

/** Called once by <ToastBridge /> after Fluent's controller is ready. */
export function registerToastDispatcher(fn: Dispatcher | null) {
  dispatcher = fn;
}

function show(intent: ToastIntent, title: string, body?: string) {
  if (!dispatcher) {
    // SSR or very-early renders before the bridge mounts. Drop silently
    // — toasts are user-facing only.
    return;
  }
  dispatcher({ title, body, intent });
}

export const toast = {
  success: (title: string, body?: string) => show("success", title, body),
  error:   (title: string, body?: string) => show("error",   title, body),
  warn:    (title: string, body?: string) => show("warning", title, body),
  info:    (title: string, body?: string) => show("info",    title, body),
};
