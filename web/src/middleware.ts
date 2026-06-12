import { NextResponse, type NextRequest } from "next/server";

// Edge middleware that injects the gateway-auth shared secret on every
// /api/* request before Next.js rewrites it to the backend.
//
// Why this lives in middleware and not the route layer:
//   - rewrites() can't modify request headers; only middleware can.
//   - Running on the edge means the header is added BEFORE the rewrite
//     fetch leaves the Next.js process, so the browser never sees it.
//   - One file, one rule — easy to audit when the security team asks
//     where the gateway secret is plumbed.
//
// The secret comes from PLOWERED_GATEWAY_SECRET on the Next.js
// container's environment. Rotate by updating the env var on both
// containers (plowered-web + plowered-api) and restarting them — old
// browser tabs will get 401s and be forced to log in again, which is
// the expected behaviour after a secret rotation.
//
// When the env var is unset the middleware is a no-op so local dev
// without nginx still works.

export function middleware(req: NextRequest) {
  if (!req.nextUrl.pathname.startsWith("/api/")) {
    return NextResponse.next();
  }
  const secret = process.env.PLOWERED_GATEWAY_SECRET;
  if (!secret) {
    return NextResponse.next();
  }
  const headers = new Headers(req.headers);
  headers.set("X-Gateway-Auth", secret);
  return NextResponse.next({ request: { headers } });
}

export const config = {
  // Only run on /api/* — every other path is a static asset or a
  // Next.js-rendered page that doesn't talk to the backend.
  matcher: ["/api/:path*"],
};
