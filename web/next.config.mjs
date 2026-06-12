/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,

  // Standalone output bundles the Next.js server + only the
  // node_modules it actually needs into .next/standalone. The
  // production Dockerfile copies that subtree into a slim runner
  // image; the resulting container is ~150MB instead of ~800MB and
  // boots in well under a second.
  output: "standalone",

  // /api/* is the BFF surface — browsers hit it on the same origin
  // as the frontend, the Next.js middleware injects the gateway
  // header server-side, and the rewrite lands on the backend.
  //
  // In production this points at the internal docker-compose service
  // (http://plowered-api:8080); in local dev it defaults to a host
  // localhost. Setting PLOWERED_API_BASE at runtime is sufficient —
  // we don't bake the URL into the build.
  async rewrites() {
    const apiBase = process.env.PLOWERED_API_BASE ?? "http://localhost:8080";
    return [{ source: "/api/:path*", destination: `${apiBase}/:path*` }];
  },
};
export default nextConfig;
