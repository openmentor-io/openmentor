/**
 * @jest-environment node
 */

interface HeaderRule {
  source: string
  headers: { key: string; value: string }[]
}

// eslint-disable-next-line @typescript-eslint/no-require-imports
const nextConfig = require('../../next.config.js') as { headers: () => Promise<HeaderRule[]> }

describe('next.config.js headers', () => {
  // Per-user BFF responses must stay out of the browser HTTP cache: an ETag
  // revalidation in Safari handed fetch() an empty 200 and broke the mentor inbox.
  it('marks every /api response no-store', async () => {
    const rules = await nextConfig.headers()
    const apiRule = rules.find((rule) => rule.source === '/api/:path*')

    expect(apiRule?.headers).toContainEqual({ key: 'Cache-Control', value: 'no-store' })
  })
})
