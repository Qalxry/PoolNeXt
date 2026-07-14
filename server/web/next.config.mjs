const devBackendUrl =
  process.env.NEXT_DEV_BACKEND_URL?.replace(/\/+$/, '') ||
  'http://127.0.0.1:3000';
const enableDevProxy = process.env.NEXT_DEV_PROXY === 'true';
const buildId = (
  process.env.NEXT_BUILD_ID ||
  process.env.NEXT_PUBLIC_APP_VERSION ||
  'dev'
)
  .replace(/[^a-zA-Z0-9._-]/g, '-')
  .slice(0, 64);

export default function createNextConfig() {
  const nextConfig = {
    output: 'export',
    generateBuildId: async () => buildId || 'dev',
    pageExtensions: ['ts', 'tsx'],
    images: {
      unoptimized: true,
    },
    eslint: {
      dirs: [
        'app',
        'components',
        'features',
        'hooks',
        'lib',
        'store',
        'tests',
        'types',
      ],
    },
  };

  if (!enableDevProxy) {
    return nextConfig;
  }

  return {
    ...nextConfig,
    async rewrites() {
      return [
        {
          source: '/api/:path*',
          destination: `${devBackendUrl}/api/:path*`,
        },
      ];
    },
  };
}
