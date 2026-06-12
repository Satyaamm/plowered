/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,

  // Standalone output bundles the Next.js server + only the
  // node_modules it actually needs into .next/standalone. The
  // production Dockerfile copies that subtree into a slim runner
  // image; the resulting container is ~150MB instead of ~800MB and
  // boots in well under a second.
  output: "standalone",

  // NOTE: the /api/* BFF rewrite deliberately does NOT live here.
  // With `output: standalone`, next.config rewrites are evaluated at
  // BUILD time and baked into the routes manifest — a runtime
  // PLOWERED_API_BASE would be silently ignored. The rewrite (and the
  // gateway-header injection) live in src/middleware.ts, which reads
  // env per request at runtime.
};
export default nextConfig;
