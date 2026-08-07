import type { NextApiRequest, NextApiResponse } from 'next'
import type { Counter, Gauge, Histogram } from 'prom-client'
import { withObservability } from '@/lib/with-observability'
import { API_ROUTE_LABELS } from '@/lib/api-routes'
import register, { activeRequests, httpRequestDuration, httpRequestTotal } from '@/lib/metrics'

const MENTOR_ID = '11111111-1111-1111-1111-111111111111'
const REQUEST_ID = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'

describe('withObservability label cardinality', () => {
  async function call(handler: ReturnType<typeof withObservability>, url: string): Promise<void> {
    // headers/socket are read by logHttpRequest.
    const req = { url, method: 'GET', headers: {}, socket: {} } as unknown as NextApiRequest
    const res = { statusCode: 200, end: () => undefined } as unknown as NextApiResponse
    await handler(req, res)
  }

  type LabelledMetric = Counter<string> | Histogram<string> | Gauge<string>

  async function routesSeenOn(metric: LabelledMetric): Promise<string[]> {
    const collected = await metric.get()
    return [...new Set(collected.values.map((v) => String(v.labels.http_route)))].sort()
  }

  async function routesSeen(): Promise<string[]> {
    return routesSeenOn(httpRequestTotal)
  }

  beforeEach(() => {
    httpRequestTotal.reset()
    httpRequestDuration.reset()
    activeRequests.reset()
  })

  it('is registered on the registry the /api/metrics endpoint scrapes', () => {
    expect(register.getSingleMetric('http_server_request_total')).toBe(httpRequestTotal)
  })

  it('labels every metric with the declared template, not the request URL', async () => {
    const handler = withObservability('/api/mentor/requests/:id/status', (_req, res) => {
      res.end()
    })

    await call(handler, `/api/mentor/requests/${REQUEST_ID}/status?from=inbox`)

    for (const metric of [httpRequestTotal, httpRequestDuration, activeRequests]) {
      expect(await routesSeenOn(metric)).toEqual(['/api/mentor/requests/:id/status'])
    }
  })

  // The metrics are labelled before the handler authenticates anything, so an
  // unauthenticated flood of unique paths must not mint a series each. Before
  // C7 these paths were normalized and mapped through an allowlist, so they at
  // least landed in a shared `other` bucket; now the URL never reaches the
  // label at all, so they land on the route's own single series.
  it('produces exactly one series for thousands of unique unknown paths', async () => {
    const handler = withObservability('/api/healthcheck', (_req, res) => {
      res.end()
    })

    for (let i = 0; i < 1000; i += 1) {
      await call(handler, `/api/${i}/not-a-route-${i}?q=${i}`)
    }

    expect(await routesSeen()).toEqual(['/api/healthcheck'])
    expect(await routesSeenOn(httpRequestDuration)).toEqual(['/api/healthcheck'])
    expect(await routesSeenOn(activeRequests)).toEqual(['/api/healthcheck'])
  })

  // The live vector P13 could only partially close: a real dynamic route reached
  // with ids that are not UUID-shaped (Airtable `rec…` ids, numeric ids). URL
  // normalization left those verbatim, so each value was its own series on each
  // of the three metrics until the allowlist folded them into `other`.
  const NON_UUID_ROUTES = [
    '/api/mentor/requests/:id',
    '/api/mentor/requests/:id/status',
    '/api/mentor/requests/:id/decline',
    '/api/admin/mentors/:id',
    '/api/admin/mentors/:id/requests/:id/status',
  ] as const

  it.each(NON_UUID_ROUTES)('holds %s to one series across 500 non-UUID ids', async (template) => {
    const handler = withObservability(template, (_req, res) => {
      res.end()
    })
    const urls = Array.from({ length: 500 }, (_, i) =>
      template.replace(/:id/g, `rec${i}AbCdEfGhIjK`)
    )

    // The premise: 500 distinct URLs, one label.
    expect(new Set(urls).size).toBe(500)

    for (const url of urls) {
      await call(handler, url)
    }

    expect(await routesSeen()).toEqual([template])
    expect(await routesSeenOn(httpRequestDuration)).toEqual([template])
    expect(await routesSeenOn(activeRequests)).toEqual([template])
  })

  // Mixed traffic: the whole label set is bounded by the declared templates,
  // and each route keeps its own readable series.
  it('bounds the label set to the declared templates under mixed traffic', async () => {
    const status = withObservability('/api/mentor/requests/:id/status', (_req, res) => {
      res.end()
    })
    const approve = withObservability('/api/admin/mentors/:id/approve', (_req, res) => {
      res.end()
    })
    const healthcheck = withObservability('/api/healthcheck', (_req, res) => {
      res.end()
    })

    for (let i = 0; i < 500; i += 1) {
      await call(status, `/api/mentor/requests/rec${i}/status`)
      await call(approve, `/api/admin/mentors/${i}/approve`)
    }
    await call(healthcheck, '/api/healthcheck')
    await call(status, `/api/mentor/requests/${REQUEST_ID}/status`)
    await call(approve, `/api/admin/mentors/${MENTOR_ID}/approve`)

    const seen = await routesSeen()
    expect(seen).toEqual([
      '/api/admin/mentors/:id/approve',
      '/api/healthcheck',
      '/api/mentor/requests/:id/status',
    ])
    // The ceiling, whatever traffic arrives: one series per declared template.
    expect(seen.length).toBeLessThanOrEqual(API_ROUTE_LABELS.length)
    expect(seen.every((route) => (API_ROUTE_LABELS as readonly string[]).includes(route))).toBe(true)
  })

  it('rejects a request-derived label in development', () => {
    const previous = process.env.NODE_ENV
    Object.defineProperty(process.env, 'NODE_ENV', { value: 'development', configurable: true })
    try {
      expect(() =>
        // The type forbids this; a JS caller or a cast is how it would happen.
        withObservability(`/api/mentor/requests/${REQUEST_ID}/status` as never, (_req, res) => {
          res.end()
        })
      ).toThrow(/not declared/)
    } finally {
      Object.defineProperty(process.env, 'NODE_ENV', { value: previous, configurable: true })
    }
  })
})
