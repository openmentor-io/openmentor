import fs from 'fs'
import path from 'path'

/**
 * Next statically optimizes a Pages-Router page only when it exports neither
 * `getServerSideProps` nor `getStaticProps`. These seven exported a
 * `getServerSideProps` whose entire body was one `logger.info` call, which
 * forced a server render — and `Cache-Control: private, no-store` — on every
 * request for content that never changes between requests (C9).
 *
 * The page-view telemetry that bought is already recorded four other ways
 * (Faro, PostHog, GTM, the proxy access log), so it is not replaced by a
 * `getStaticProps`: adding one would keep the pages out of the static export
 * for a counter that ticks once per build.
 */
const STATIC_PAGES = [
  'about',
  'bementor',
  'donate',
  'faq',
  'migrate',
  'privacy',
  'terms',
] as const

/**
 * Pages that legitimately need per-request data, with the reason. Listed so
 * that turning one of them static — which would serve one visitor's data to
 * the next — has to be a deliberate edit to this file.
 */
const SSR_PAGES: Record<string, string> = {
  'index.tsx': 'fetches the mentor catalog',
  'mentors/index.tsx': 'fetches the mentor catalog',
  'mentors/[tag].tsx': 'fetches the catalog and 404s an unknown tag',
  'mentor/[slug]/index.tsx': 'fetches one mentor, 404s/redirects on renamed slugs',
  'mentor/[slug]/contact.tsx': 'fetches one mentor and its calendar URL',
  'sitemap.xml.ts': 'writes the XML body onto the response',
}

function source(relative: string): string {
  return fs.readFileSync(path.join(process.cwd(), 'src/pages', relative), 'utf8')
}

describe('statically optimized content pages', () => {
  it.each(STATIC_PAGES)('%s exports no data-fetching function', (page) => {
    const code = source(`${page}.tsx`)

    expect(code).not.toMatch(/\bgetServerSideProps\b/)
    expect(code).not.toMatch(/\bgetStaticProps\b/)
  })

  it.each(STATIC_PAGES)('%s pulls in no server-only logger', (page) => {
    const code = source(`${page}.tsx`)

    // The log line was the only reason these pages imported the winston logger;
    // leaving the import would pull the server transports into the client
    // bundle of a page that no longer has a server half.
    expect(code).not.toContain("from '@/lib/logger'")
    expect(code).not.toContain('with-ssr-observability')
  })
})

describe('pages that must stay server-rendered', () => {
  it.each(Object.entries(SSR_PAGES))('%s still exports getServerSideProps (%s)', (page) => {
    expect(source(page)).toMatch(/export const getServerSideProps/)
  })
})
