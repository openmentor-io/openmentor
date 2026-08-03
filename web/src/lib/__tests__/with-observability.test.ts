import fs from 'fs'
import path from 'path'
import type { NextApiRequest, NextApiResponse } from 'next'
import { normalizeRoute, routeLabel, withObservability } from '@/lib/with-observability'
import register, { httpRequestTotal } from '@/lib/metrics'

const MENTOR_ID = '11111111-1111-1111-1111-111111111111'
const REQUEST_ID = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'

describe('normalizeRoute', () => {
  // http_route labels a counter, a histogram and a gauge. Any raw id that
  // survives here mints a new series per request — unbounded cardinality.
  it.each([
    [`/api/admin/mentors/${MENTOR_ID}/requests`, '/api/admin/mentors/:id/requests'],
    [`/api/admin/mentors/${MENTOR_ID}/requests/${REQUEST_ID}`, '/api/admin/mentors/:id/requests/:id'],
    [
      `/api/admin/mentors/${MENTOR_ID}/requests/${REQUEST_ID}/status`,
      '/api/admin/mentors/:id/requests/:id/status',
    ],
    [`/api/admin/mentors/${MENTOR_ID}`, '/api/admin/mentors/:id'],
    [`/api/admin/mentors/${MENTOR_ID}/approve`, '/api/admin/mentors/:id/approve'],
    [`/api/admin/mentors/${MENTOR_ID}/username/availability`, '/api/admin/mentors/:id/username/availability'],
    [`/api/mentor/requests/${REQUEST_ID}`, '/api/mentor/requests/:id'],
    [`/api/mentor/requests/${REQUEST_ID}/status`, '/api/mentor/requests/:id/status'],
    [`/api/mentor/requests/${REQUEST_ID}/decline`, '/api/mentor/requests/:id/decline'],
  ])('normalizes every id segment in %s', (url, expected) => {
    expect(normalizeRoute(url)).toBe(expected)
  })

  it('leaves no raw UUID in any admin or mentor request route', () => {
    const uuid = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i
    const urls = [
      `/api/admin/mentors/${MENTOR_ID}/requests/${REQUEST_ID}`,
      `/api/admin/mentors/${MENTOR_ID}/requests/${REQUEST_ID}/status`,
      `/api/mentor/requests/${REQUEST_ID}/status`,
    ]
    for (const url of urls) {
      expect(normalizeRoute(url)).not.toMatch(uuid)
    }
  })

  it('produces one label for many distinct request ids', () => {
    const ids = [REQUEST_ID, '00000000-0000-4000-8000-000000000000', 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee']
    const labels = new Set(
      ids.map((id) => normalizeRoute(`/api/admin/mentors/${MENTOR_ID}/requests/${id}/status`))
    )
    expect(labels.size).toBe(1)
  })

  it('strips the query string', () => {
    expect(normalizeRoute('/api/admin/mentors?status=pending')).toBe('/api/admin/mentors')
    expect(normalizeRoute(`/api/admin/mentors/${MENTOR_ID}/requests?status=declined`)).toBe(
      '/api/admin/mentors/:id/requests'
    )
  })

  // The slug rule is for public profile pages; it must not rewrite the fixed
  // segments of /api/mentor/* routes.
  it('keeps the slug rule off API routes', () => {
    expect(normalizeRoute('/api/mentor/requests')).toBe('/api/mentor/requests')
    expect(normalizeRoute('/api/mentor/profile/picture')).toBe('/api/mentor/profile/picture')
    expect(normalizeRoute('/mentor/john-doe-42')).toBe('/mentor/:slug')
    expect(normalizeRoute('/mentor/john-doe-42/contact')).toBe('/mentor/:slug/contact')
  })

  it('leaves static routes and empty input alone', () => {
    expect(normalizeRoute('/api/healthcheck')).toBe('/api/healthcheck')
    expect(normalizeRoute('/api/admin/auth/session')).toBe('/api/admin/auth/session')
    expect(normalizeRoute('')).toBe('unknown')
  })
})

/**
 * Every instrumented route file, as the template normalizeRoute produces for it:
 * a route that drops off the allowlist silently loses its own series, and the
 * dashboards read http_route by name.
 */
function instrumentedRoutes(): string[] {
  const root = path.join(process.cwd(), 'src/pages/api')
  const templates: string[] = []

  const walk = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name)
      if (entry.isDirectory()) {
        walk(full)
        continue
      }
      if (!/\.tsx?$/.test(entry.name)) continue
      if (!fs.readFileSync(full, 'utf8').includes('withObservability(')) continue
      templates.push(
        '/api' +
          full
            .slice(root.length)
            .replace(/\.tsx?$/, '')
            .replace(/\/index$/, '')
            // [id], [requestId], … all normalize to :id
            .replace(/\/\[[^\]]+\]/g, '/:id')
      )
    }
  }

  walk(root)
  return templates.sort()
}

/** A concrete URL for a route template, with a distinct id per :id segment. */
function urlFor(template: string): string {
  let n = 0
  return template.replace(/:id/g, () => {
    n += 1
    return `1b0c9e42-a072-47ed-8ce9-edd30687400${n}`
  })
}

describe('routeLabel', () => {
  it.each(instrumentedRoutes())('labels %s as itself', (template) => {
    expect(routeLabel(urlFor(template))).toBe(template)
  })

  it('keeps the query string out of the label', () => {
    expect(routeLabel('/api/username-availability?u=jane')).toBe('/api/username-availability')
  })

  it.each([
    '/api/wp-login.php',
    '/api/../../etc/passwd',
    '/api/mentor/profile/../../../admin',
    '/api/v1/graphql',
    '/mentor/john-doe-42',
    '',
  ])('labels the unrecognised path %s as `other`', (url) => {
    expect(routeLabel(url)).toBe('other')
  })
})

describe('withObservability label cardinality', () => {
  const handler = withObservability((_req, res) => {
    res.end()
  })

  async function call(url: string): Promise<void> {
    // headers/socket are read by logHttpRequest.
    const req = { url, method: 'GET', headers: {}, socket: {} } as unknown as NextApiRequest
    const res = { statusCode: 200, end: () => undefined } as unknown as NextApiResponse
    await handler(req, res)
  }

  async function routesSeen(): Promise<string[]> {
    const metric = await httpRequestTotal.get()
    return [...new Set(metric.values.map((v) => String(v.labels.http_route)))].sort()
  }

  beforeEach(() => {
    httpRequestTotal.reset()
  })

  it('is registered on the registry the /api/metrics endpoint scrapes', () => {
    expect(register.getSingleMetric('http_server_request_total')).toBe(httpRequestTotal)
  })

  // The metrics are labelled before the handler authenticates anything, so an
  // unauthenticated flood of unique paths must not mint a series each.
  it('produces one series for thousands of unique unknown paths', async () => {
    for (let i = 0; i < 1000; i += 1) {
      await call(`/api/${i}/not-a-route-${i}?q=${i}`)
    }

    expect(await routesSeen()).toEqual(['other'])
  })

  it('still gives real routes their own readable labels', async () => {
    await call('/api/contact-mentor')
    await call(`/api/mentor/requests/${REQUEST_ID}/status`)
    await call(`/api/admin/mentors/${MENTOR_ID}/requests/${REQUEST_ID}`)
    await call('/api/nope')

    expect(await routesSeen()).toEqual([
      '/api/admin/mentors/:id/requests/:id',
      '/api/contact-mentor',
      '/api/mentor/requests/:id/status',
      'other',
    ])
  })
})
