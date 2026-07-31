import { normalizeRoute } from '@/lib/with-observability'

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
