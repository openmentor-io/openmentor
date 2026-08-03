const { withPostHogConfig } = require('@posthog/nextjs-config')

const nextConfig = {
  // Enable standalone output for Docker deployments
  output: 'standalone',

  // The social-card renderer (/api/og/mentor) needs runtime files the
  // standalone trace misses: its TTF fonts (read from disk) and next/og's
  // compiled @vercel/og package (satori + resvg/yoga wasm — the tracer does
  // not follow the `next/og` import, so without this the endpoint throws
  // MODULE_NOT_FOUND in the Docker image and falls back to the banner).
  outputFileTracingIncludes: {
    '/api/og/mentor': [
      './src/assets/og-fonts/*.ttf',
      './node_modules/next/dist/compiled/@vercel/og/**/*',
    ],
  },

  // @marsidev/react-turnstile ships ESM-only; transpile it so Jest
  // (via next/jest) and the server build can consume it.
  transpilePackages: ['@marsidev/react-turnstile'],

  images: {
    // Entries with an unset/empty hostname are dropped: Next.js rejects
    // empty remotePattern hostnames at build time.
    remotePatterns: [
      {
        protocol: 'https',
        hostname: process.env.NEXT_PUBLIC_S3_STORAGE_ENDPOINT,
        port: '',
        pathname: `/${process.env.NEXT_PUBLIC_S3_STORAGE_BUCKET || 'mentor-images'}/**`,
      },
      {
        protocol: 'https',
        hostname: process.env.NEXT_PUBLIC_CDN_ENDPOINT,
        port: '',
        pathname: `/**`,
      },
    ].filter((pattern) => pattern.hostname),
  },

  experimental: {
    largePageDataBytes: 10 * 1024 * 1024,
  },

  // Next.js 16 way to exclude server-side packages from bundling
  // These packages use Node.js built-ins and should be loaded at runtime
  serverExternalPackages: [
    // OpenTelemetry packages
    '@opentelemetry/sdk-node',
    '@opentelemetry/auto-instrumentations-node',
    '@opentelemetry/exporter-trace-otlp-http',
    '@opentelemetry/resources',
    // Prometheus metrics
    'prom-client',
    // Winston logger
    'winston',
    // PostHog server-side SDK
    'posthog-node',
  ],

  // Enable Turbopack (Next.js 16 default)
  turbopack: {},

  async headers() {
    const headers = [
      // this header fixed bad behaviors of next <Image /> component
      // now local images from /images directory will be cached for 1 day
      // otherwise cache image will regenerate every 60 seconds
      {
        source: '/images/(.*)',
        headers: [
          {
            key: 'cache-control',
            value: 'public, max-age=86400, must-revalidate',
          },
        ],
      },
    ]

    // Add security headers everywhere except local development (the Next dev
    // server needs 'unsafe-eval'/inline for HMR, which the strict CSP forbids).
    if (process.env.NODE_ENV !== 'development') {
      headers.push({
        source: '/:path*',
        headers: [
          {
            key: 'X-DNS-Prefetch-Control',
            value: 'on',
          },
          {
            key: 'X-Frame-Options',
            value: 'SAMEORIGIN',
          },
          {
            key: 'X-Content-Type-Options',
            value: 'nosniff',
          },
          {
            key: 'Referrer-Policy',
            value: 'strict-origin-when-cross-origin',
          },
          {
            key: 'Permissions-Policy',
            value: 'camera=(), microphone=(), geolocation=(), interest-cohort=()',
          },
          {
            key: 'Content-Security-Policy',
            value:
              "default-src 'self'; " +
              // SECURITY (M8): no 'unsafe-eval' — GTM/PostHog/Faro don't need it
              // and Next's production runtime doesn't either. 'unsafe-inline'
              // remains until inline scripts move to nonces/strict-dynamic.
              "script-src 'self' 'unsafe-inline' https://openmentor.io https://challenges.cloudflare.com https://www.googletagmanager.com https://www.google-analytics.com https://a.openmentor.io https://us.i.posthog.com https://eu.i.posthog.com https://us-assets.i.posthog.com https://eu-assets.i.posthog.com https://faro-collector-prod-eu-west-3.grafana.net; " +
              "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
              "img-src 'self' https: data:; " +
              "font-src 'self' data: https://fonts.gstatic.com https://at.alicdn.com; " +
              "connect-src 'self' https://a.openmentor.io https://us.i.posthog.com https://eu.i.posthog.com https://eu.posthog.com https://openmentor.io https://www.google-analytics.com https://region1.analytics.google.com https://stats.g.doubleclick.net https://google.com https://www.google.com https://faro-collector-prod-eu-west-3.grafana.net https://region1.google-analytics.com; " +
              // mentor calendars (CalendarType 'calendlab')
              'frame-src https://challenges.cloudflare.com https://calendly.com https://koalendar.com https://calendlab.ru https://docs.google.com; ' +
              "object-src 'none'; " +
              "base-uri 'self'; " +
              "form-action 'self'; " +
              // SECURITY (M8): clickjacking defense alongside X-Frame-Options.
              "frame-ancestors 'self'; " +
              'upgrade-insecure-requests;',
          },
        ],
      })
    }

    return headers
  },

  async redirects() {
    return [
      {
        source: '/:slug([a-z-]+\\d+)',
        destination: '/mentor/:slug', // Matched parameters can be used in the destination
        permanent: true,
      },
    ]
  },

  onDemandEntries: {
    // period (in ms) where the server will keep pages in the buffer
    maxInactiveAge: 60 * 60 * 1000,
    // number of pages that should be kept simultaneously without being disposed
    pagesBufferLength: 20,
  },

  async rewrites() {
    const rewrites = []

    // Proxy Faro telemetry to Grafana Cloud to bypass CORS
    // Browser sends to /faro-collect -> Next.js rewrites to Grafana Cloud
    if (process.env.NEXT_PUBLIC_FARO_COLLECTOR_URL) {
      rewrites.push({
        source: '/faro-collect',
        destination: process.env.NEXT_PUBLIC_FARO_COLLECTOR_URL,
      })
    }

    return rewrites
  },

}

const posthogUploadEnabled = !!(
  process.env.POSTHOG_PERSONAL_API_KEY && process.env.POSTHOG_PROJECT_ID
)

module.exports = posthogUploadEnabled
  ? withPostHogConfig(nextConfig, {
      personalApiKey: process.env.POSTHOG_PERSONAL_API_KEY,
      projectId: process.env.POSTHOG_PROJECT_ID,
      host: process.env.NEXT_PUBLIC_POSTHOG_HOST || 'https://eu.i.posthog.com',
      sourcemaps: {
        releaseName: 'openmentor-frontend',
        releaseVersion: process.env.NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION || 'unknown',
        deleteAfterUpload: true,
      },
    })
  : nextConfig
