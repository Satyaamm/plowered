// Tiny fetch helper shared across hooks. The session rides on the
// plowered_session HttpOnly cookie — `credentials: "include"` ensures
// the browser sends it on every request through the Next.js rewrite.

// ApiError carries everything the API tells us about a failed request:
// the human-readable `message`, the machine-friendly `code` (e.g.
// "validation_failed", "forbidden", "rate_limited"), the HTTP status,
// and the path + method that produced it. The toast layer uses `code`
// for a smarter title than a hardcoded "Action failed".
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  readonly method: string;
  readonly path: string;
  constructor(opts: {
    message: string;
    code: string;
    status: number;
    method: string;
    path: string;
  }) {
    super(opts.message);
    this.name = "ApiError";
    this.code = opts.code;
    this.status = opts.status;
    this.method = opts.method;
    this.path = opts.path;
  }
}

export async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: "include",
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new ApiError({
      message: err.message ?? `HTTP ${res.status}`,
      code: err.error ?? `http_${res.status}`,
      status: res.status,
      method,
      path,
    });
  }
  if (res.status === 204) return undefined as unknown as T;
  return (await res.json()) as T;
}
