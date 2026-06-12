import { NextResponse, type NextRequest } from "next/server";

// The BFF proxy. Every /api/* request is rewritten server-side to the
// backend with the gateway-auth header injected — browsers stay on the
// web origin and never see the secret or the internal API address.
//
// Why the REWRITE lives here and not in next.config rewrites():
//   - With `output: standalone`, next.config rewrites are evaluated at
//     BUILD time and baked into the routes manifest. A runtime
//     PLOWERED_API_BASE is silently ignored, which breaks the moment
//     the build host's env differs from the runtime container's
//     (exactly what happened in the first VPS deploy — the
//     destination froze as http://localhost:8080 and every /api/*
//     call 500'd). Middleware runs per request and reads process.env
//     at RUNTIME, so the same image works in any environment.
//   - Header injection has to happen in middleware anyway (config
//     rewrites can't modify request headers), so doing both here
//     keeps the entire BFF in one auditable file.
//
// Env contract (both read at runtime, on the web container):
//   PLOWERED_API_BASE       — internal backend origin, e.g.
//                             http://plowered-api:8080 (compose) or
//                             http://localhost:8080 (local dev default)
//   PLOWERED_GATEWAY_SECRET — shared secret the API's GatewayAuthMW
//                             expects; unset = header not added (local
//                             dev without nginx still works).

export function middleware(req: NextRequest) {
  const { pathname, search } = req.nextUrl;
  if (!pathname.startsWith("/api/")) {
    return NextResponse.next();
  }

  const apiBase = process.env.PLOWERED_API_BASE ?? "http://localhost:8080";
  // Strip the /api prefix: /api/v1/auth/me → <base>/v1/auth/me
  const target = new URL(pathname.slice("/api".length) + search, apiBase);

  const headers = new Headers(req.headers);
  const secret = process.env.PLOWERED_GATEWAY_SECRET;
  if (secret) {
    headers.set("X-Gateway-Auth", secret);
  }
  return NextResponse.rewrite(target, { request: { headers } });
}

export const config = {
  // Only run on /api/* — every other path is a static asset or a
  // Next.js-rendered page that doesn't talk to the backend.
  matcher: ["/api/:path*"],
};
