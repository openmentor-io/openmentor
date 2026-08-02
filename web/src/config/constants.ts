interface ConstantsConfig {
  BASE_URL: string | undefined
  CACHE_REFRESH_INTERVAL: number
}

// DOMAIN is server-only (no NEXT_PUBLIC_ prefix), so Next inlines it as
// undefined in the client bundle. The literal fallback keeps BASE_URL a valid
// origin there — without it every client-rendered canonical/og:url would read
// `https://undefined/`.
//
// KNOWN LIMITATION: on a deployment whose DOMAIN is not openmentor.io, the
// server honours it but the client cannot see it, so client-side navigations
// emit openmentor.io URLs. Fixing that needs a build-time public origin
// (NEXT_PUBLIC_SITE_URL) threaded through infra/.env*.example and env.d.ts.
const PRODUCTION_ORIGIN = 'https://openmentor.io/'

const constants: ConstantsConfig = {
  BASE_URL:
    process.env.NODE_ENV === 'production'
      ? process.env.DOMAIN
        ? `https://${process.env.DOMAIN}/`
        : PRODUCTION_ORIGIN
      : 'http://localhost:3000/',
  CACHE_REFRESH_INTERVAL: 1 * 60 * 1000,
}

export default constants
