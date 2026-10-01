/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  experimental: {
    // /api is proxied to the Go backend; Next's default 30s proxy timeout
    // would cut off large APK installs and file pushes to devices.
    proxyTimeout: 10 * 60 * 1000,
  },
  // Behind caddy (the normal deployment) /api and /ws go straight to the
  // backend; these rewrites serve `next dev` and direct use of port 3000.
  async rewrites() {
    return [
      {
        source: '/api/:path*',
        destination: 'http://localhost:8080/api/:path*',
      },
      {
        source: '/ws/:path*',
        destination: 'http://localhost:8080/ws/:path*',
      },
    ];
  },
};

module.exports = nextConfig;
