/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  async rewrites() {
    const apiBase = process.env.PLOWERED_API_BASE ?? "http://localhost:8080";
    return [{ source: "/api/:path*", destination: `${apiBase}/:path*` }];
  },
};
export default nextConfig;
