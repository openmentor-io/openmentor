import fs from 'fs'
import path from 'path'
import { API_ROUTE_LABELS, assertApiRouteLabel } from '@/lib/api-routes'

const API_ROOT = path.join(process.cwd(), 'src/pages/api')

/**
 * `/api/metrics` is the Prometheus scrape target itself. Instrumenting it would
 * have every scrape write to the registry it is about to serialize, so it is
 * deliberately uninstrumented and carries no `http_route` label.
 */
const UNINSTRUMENTED = new Set(['/api/metrics'])

interface RouteFile {
  /** Route template derived from the file path: `[id]`/`[requestId]` -> `:id`. */
  template: string
  source: string
  file: string
}

function routeFiles(): RouteFile[] {
  const found: RouteFile[] = []

  const walk = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const full = path.join(dir, entry.name)
      if (entry.isDirectory()) {
        walk(full)
        continue
      }
      if (!/\.tsx?$/.test(entry.name)) continue
      found.push({
        template:
          '/api' +
          full
            .slice(API_ROOT.length)
            .replace(/\.tsx?$/, '')
            .replace(/\/index$/, '')
            .replace(/\/\[[^\]]+\]/g, '/:id'),
        source: fs.readFileSync(full, 'utf8'),
        file: path.relative(process.cwd(), full),
      })
    }
  }

  walk(API_ROOT)
  return found
}

const ROUTE_FILES = routeFiles()

describe('API_ROUTE_LABELS', () => {
  it('is sorted and free of duplicates', () => {
    const labels = [...API_ROUTE_LABELS]
    expect(labels).toEqual([...new Set(labels)])
    expect(labels).toEqual([...labels].sort())
  })

  // The labels are Grafana dimensions; a label for a route that no longer
  // exists is a panel series that silently stops.
  it('declares no label without a route file behind it', () => {
    const templates = new Set(ROUTE_FILES.map((route) => route.template))
    expect(API_ROUTE_LABELS.filter((label) => !templates.has(label))).toEqual([])
  })

  it('declares a label for every instrumented route file', () => {
    const declared = new Set<string>(API_ROUTE_LABELS)
    const missing = ROUTE_FILES.filter(
      (route) => !UNINSTRUMENTED.has(route.template) && !declared.has(route.template)
    ).map((route) => route.file)

    expect(missing).toEqual([])
  })
})

describe('assertApiRouteLabel', () => {
  const previousEnv = process.env.NODE_ENV

  afterEach(() => {
    // NODE_ENV is readonly in @types/node; the tests need to flip it.
    Object.defineProperty(process.env, 'NODE_ENV', { value: previousEnv, configurable: true })
  })

  function setEnv(value: string): void {
    Object.defineProperty(process.env, 'NODE_ENV', { value, configurable: true })
  }

  it('accepts every declared label', () => {
    setEnv('development')
    for (const label of API_ROUTE_LABELS) {
      expect(() => assertApiRouteLabel(label)).not.toThrow()
    }
  })

  it.each([
    ['', 'empty'],
    ['   ', 'whitespace only'],
  ])('rejects the %p label in development (%s)', (label) => {
    setEnv('development')
    expect(() => assertApiRouteLabel(label)).toThrow(/route label is empty/)
  })

  // The failure this guards: someone reaches for `req.url` again, and the
  // label goes back to being unbounded attacker-controlled input.
  it.each([
    '/api/mentor/requests/1b0c9e42-a072-47ed-8ce9-edd306874ec9/status',
    '/api/admin/mentors/42/approve',
    '/api/username-availability?u=jane',
    '/api/Mentor/Profile',
    '/api/wp-login.php',
    '/mentor/john-doe-42',
    'other',
  ])('rejects the request-derived label %p in development', (label) => {
    setEnv('development')
    expect(() => assertApiRouteLabel(label)).toThrow(/not declared/)
  })

  // A labelling mistake must not take a live endpoint down at module load.
  it('never throws in production', () => {
    setEnv('production')
    for (const label of ['', '/api/admin/mentors/42/approve', 'other']) {
      expect(() => assertApiRouteLabel(label)).not.toThrow()
    }
  })
})
