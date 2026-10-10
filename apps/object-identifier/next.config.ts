import type { NextConfig } from "next";

/** Upstream Go Image API origin (no /api/v1 suffix). */
const imageApiOrigin = (
  process.env.IMAGE_API_ORIGIN || "http://localhost:8080"
).replace(/\/$/, "");

const nextConfig: NextConfig = {
  async rewrites() {
    // Browser calls same-origin /api/v1/* ; Next/Netlify proxies to Render (or local Go).
    // Keeps /api/identify (Gemini) on this app — only /api/v1 is forwarded.
    return [
      {
        source: "/api/v1/:path*",
        destination: `${imageApiOrigin}/api/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
