import { createMocks, type RequestMethod } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

jest.mock('@/lib/logger', () => ({
  __esModule: true,
  default: { info: jest.fn(), warn: jest.fn(), error: jest.fn() },
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

// satori + resvg are the cost this endpoint is being bounded against; the tests
// count invocations rather than paying it.
const mockImageResponse = jest.fn()
jest.mock('next/og', () => ({
  ImageResponse: jest.fn().mockImplementation((...args: unknown[]) => {
    mockImageResponse(...args)
    return { arrayBuffer: async () => new Uint8Array([0x89, 0x50, 0x4e, 0x47]).buffer }
  }),
}))

import handler, { __resetCardCache } from '@/pages/api/og/mentor'

// jsdom has AbortSignal but not the static timeout(), which both upstream
// fetches use. Without this every request throws and lands on the banner, so
// the happy-path assertions below would pass for the wrong reason.
if (typeof AbortSignal.timeout !== 'function') {
  AbortSignal.timeout = ((ms: number) => {
    const controller = new AbortController()
    setTimeout(() => controller.abort(), ms).unref?.()
    return controller.signal
  }) as typeof AbortSignal.timeout
}

const MENTOR = {
  mentorId: '550e8400-e29b-41d4-a716-446655440001',
  name: 'Ada Lovelace',
  job: 'Staff Engineer',
  workplace: 'Tech Corp',
  experience: '10+',
  price: 'Free',
  slug: 'ada-lovelace-1',
  updatedAt: '2026-07-08T11:22:33.444Z',
}

const mockFetch = jest.fn()

/** Upstream mentor lookup + photo fetch, both of which C8 gates behind bounds. */
function stubUpstream(): void {
  mockFetch.mockImplementation(async (url: string) => {
    if (String(url).includes('/internal/mentors')) {
      return { ok: true, json: async () => MENTOR, headers: new Map() }
    }
    // No photo in storage -> the initials fallback, no image bytes to fake.
    return { ok: false, headers: { get: () => null } }
  })
}

async function call(
  query: Record<string, string | string[]>,
  method: RequestMethod = 'GET',
): Promise<ReturnType<typeof createMocks>['res']> {
  const { req, res } = createMocks({ method, url: '/api/og/mentor', query })
  await handler(req as unknown as NextApiRequest, res as unknown as NextApiResponse)
  return res
}

beforeEach(() => {
  __resetCardCache()
  mockImageResponse.mockClear()
  mockFetch.mockReset()
  stubUpstream()
  global.fetch = mockFetch as unknown as typeof global.fetch
})

describe('api/og/mentor bounds', () => {
  it.each<RequestMethod>(['POST', 'PUT', 'DELETE', 'PATCH'])(
    'refuses %s without doing any work',
    async (method) => {
      const res = await call({ slug: 'ada-lovelace-1' }, method)

      expect(res.statusCode).toBe(405)
      expect(res._getHeaders()['allow']).toBe('GET, HEAD')
      expect(mockFetch).not.toHaveBeenCalled()
      expect(mockImageResponse).not.toHaveBeenCalled()
    },
  )

  it.each([
    ['a missing slug', {}],
    ['an empty slug', { slug: '' }],
    ['a traversal attempt', { slug: '../../../etc/passwd' }],
    ['a path segment', { slug: 'ada/lovelace' }],
    ['uppercase', { slug: 'Ada-Lovelace-1' }],
    ['a query smuggled into the slug', { slug: 'ada?x=1' }],
    ['a repeated slug param', { slug: ['ada-lovelace-1', 'other-1'] }],
    ['65 characters', { slug: 'a'.repeat(65) }],
  ])('sheds %s to the banner before any upstream call', async (_label, query) => {
    const res = await call(query as Record<string, string | string[]>)

    expect(res.statusCode).toBe(302)
    expect(res._getRedirectUrl()).toBe('/images/banner.png')
    // A transient/refused card must not be cached as this mentor's image.
    expect(res._getHeaders()['cache-control']).toBe('no-store')
    expect(mockFetch).not.toHaveBeenCalled()
    expect(mockImageResponse).not.toHaveBeenCalled()
  })

  // A leading-hyphen slug exists in production: it predates the D29 username
  // grammar, so the bound is charset+length, not the grammar.
  it.each(['ada-lovelace-1', '-mariia-sergeeva-483', 'abc', 'a'.repeat(64)])(
    'accepts the live-shaped slug %p',
    async (slug) => {
      const res = await call({ slug })

      expect(res.statusCode).toBe(200)
      expect(res._getHeaders()['content-type']).toBe('image/png')
    },
  )

  it.each([
    ['not a number', 'abcdef'],
    ['too long', '1'.repeat(13)],
    ['repeated', ['1', '2']],
  ])('refuses a %s cache-buster before any upstream call', async (_label, v) => {
    const res = await call({ slug: 'ada-lovelace-1', v } as Record<string, string | string[]>)

    expect(res.statusCode).toBe(302)
    expect(mockFetch).not.toHaveBeenCalled()
  })

  it('accepts the cache-buster the mentor page actually sends', async () => {
    const res = await call({ slug: 'ada-lovelace-1', v: '1783946553' })

    expect(res.statusCode).toBe(200)
  })
})

describe('api/og/mentor caching', () => {
  it('renders once and serves the cached PNG afterwards', async () => {
    const first = await call({ slug: 'ada-lovelace-1' })
    const second = await call({ slug: 'ada-lovelace-1' })

    expect(first.statusCode).toBe(200)
    expect(second.statusCode).toBe(200)
    expect(second._getHeaders()['content-type']).toBe('image/png')
    expect(mockImageResponse).toHaveBeenCalledTimes(1)
    expect(mockFetch).toHaveBeenCalledTimes(2) // mentor + photo, once
  })

  // Arbitrary `v` values multiply CDN cache keys; they must not multiply
  // renders, which is why the application cache is keyed by slug alone.
  it('renders once across a sweep of cache-buster values', async () => {
    for (let i = 0; i < 50; i += 1) {
      const res = await call({ slug: 'ada-lovelace-1', v: String(1783946553 + i) })
      expect(res.statusCode).toBe(200)
    }

    expect(mockImageResponse).toHaveBeenCalledTimes(1)
  })

  it('bounds the cache, so a wide sweep cannot grow it without limit', async () => {
    // 17 distinct slugs against a 16-entry ceiling: the first is evicted and
    // has to re-render, proving the ceiling is enforced rather than aspirational.
    for (let i = 0; i < 17; i += 1) {
      await call({ slug: `mentor-number-${i}` })
    }
    expect(mockImageResponse).toHaveBeenCalledTimes(17)

    await call({ slug: 'mentor-number-0' })
    expect(mockImageResponse).toHaveBeenCalledTimes(18)

    await call({ slug: 'mentor-number-16' })
    expect(mockImageResponse).toHaveBeenCalledTimes(18)
  })
})

describe('api/og/mentor concurrency', () => {
  it('sheds to the banner past the render ceiling instead of piling up', async () => {
    // Hold the upstream fetch open so renders stay in flight.
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    mockFetch.mockImplementation(async (url: string) => {
      await gate
      if (String(url).includes('/internal/mentors')) {
        return { ok: true, json: async () => MENTOR, headers: new Map() }
      }
      return { ok: false, headers: { get: () => null } }
    })

    const inFlight = [
      call({ slug: 'mentor-one-1' }),
      call({ slug: 'mentor-two-2' }),
      call({ slug: 'mentor-three-3' }),
    ]
    // Let the three reach their awaits before the fourth arrives.
    await Promise.resolve()

    const shed = await call({ slug: 'mentor-four-4' })
    expect(shed.statusCode).toBe(302)
    expect(shed._getRedirectUrl()).toBe('/images/banner.png')
    expect(shed._getHeaders()['cache-control']).toBe('no-store')

    release?.()
    for (const res of await Promise.all(inFlight)) {
      expect(res.statusCode).toBe(200)
    }
    // Only the three admitted requests rendered.
    expect(mockImageResponse).toHaveBeenCalledTimes(3)
  })

  it('releases render slots once a render fails', async () => {
    mockFetch.mockRejectedValue(new Error('upstream down'))

    for (let i = 0; i < 10; i += 1) {
      const res = await call({ slug: `mentor-number-${i}` })
      expect(res.statusCode).toBe(302)
    }

    // A leaked slot would have shed request 4 onwards at capacity forever.
    stubUpstream()
    const res = await call({ slug: 'ada-lovelace-1' })
    expect(res.statusCode).toBe(200)
  })
})
