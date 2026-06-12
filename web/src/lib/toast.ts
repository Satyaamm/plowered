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
  /** Override the default success title. */
  successMessage?: string;
  /** Override the default error title. The error message itself becomes
   *  the toast body. When unset, the title is derived from the API's
   *  structured error code (e.g. "forbidden" → "Permission denied"). */
  errorTitle?: string;
  /** Override the default error body (otherwise we surface error.message). */
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

// titleForErrorCode maps the API's structured error codes to a friendly
// toast title. Unknown codes get a generic fall-through; the error
// message still lands in the body so the user sees specifics.
const ERROR_TITLES: Record<string, string> = {
  validation_failed:   "Check the form",
  bad_request:         "Check the request",
  unauthorized:        "Sign in required",
  forbidden:           "Permission denied",
  not_found:           "Not found",
  conflict:            "Conflict",
  rate_limited:        "Slow down",
  too_many_requests:   "Slow down",
  payment_required:    "Quota exceeded",
  gone:                "Resource gone",
  unsupported:         "Not supported here",
  upstream_error:      "Upstream service failed",
  upstream_unavailable:"Upstream service unavailable",
  timeout:             "Request timed out",
  gateway_required:    "Edge auth missing",
  email_not_verified:  "Verify your email",
  account_locked:      "Account locked",
  password_too_weak:   "Password too weak",
  invalid_credentials: "Wrong email or password",
  invalid_token:       "Link expired",
  // Catch-all for our http_<status> auto-codes.
};

export function deriveErrorTitle(code: string, status: number): string {
  if (ERROR_TITLES[code]) return ERROR_TITLES[code];
  if (status >= 500) return "Server error";
  if (status === 0) return "Network error";
  return "Action failed";
}
