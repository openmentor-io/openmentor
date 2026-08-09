/**
 * The closed set of `http_route` label values the Next.js API routes emit.
 *
 * P13 derived this label from `req.url` and mapped anything unrecognised to
 * `other`, which capped cardinality but meant the label was still a function of
 * attacker-controlled input. C7 makes it a compile-time argument instead:
 * `withObservability('/api/mentor/requests/:id', handler)`. Cardinality is now
 * bounded by the number of call sites, and a flood of unique paths mints no
 * series at all rather than inflating `other`.
 *
 * These strings are live Grafana dashboard dimensions — `grafana/dashboards/
 * om-frontend.json` queries `http_server_request_total` by `http_route`. They
 * are byte-for-byte the templates P13's allowlist emitted, so no existing
 * series moved; `/api/og/mentor` is newly instrumented and is additive (the
 * panel is `topk(10, sum by (http_route) ...)`, so no dashboard edit follows).
 *
 * Every dynamic segment is `:id` regardless of its file name (`[id]`,
 * `[requestId]`), because a per-parameter name would put two labels on what is
 * one route shape.
 */
export const API_ROUTE_LABELS = [
  '/api/admin/auth/logout',
  '/api/admin/auth/request-login',
  '/api/admin/auth/session',
  '/api/admin/auth/verify',
  '/api/admin/mentors',
  '/api/admin/mentors/:id',
  '/api/admin/mentors/:id/approve',
  '/api/admin/mentors/:id/decline',
  '/api/admin/mentors/:id/delete',
  '/api/admin/mentors/:id/picture',
  '/api/admin/mentors/:id/requests',
  '/api/admin/mentors/:id/requests/:id',
  '/api/admin/mentors/:id/requests/:id/status',
  '/api/admin/mentors/:id/restore',
  '/api/admin/mentors/:id/return',
  '/api/admin/mentors/:id/status',
  '/api/admin/mentors/:id/username',
  '/api/contact-mentor',
  '/api/healthcheck',
  '/api/mentor/auth/logout',
  '/api/mentor/auth/request-login',
  '/api/mentor/auth/session',
  '/api/mentor/auth/verify',
  '/api/mentor/confirm',
  '/api/mentor/confirm-resend',
  '/api/mentor/profile',
  '/api/mentor/profile/delete',
  '/api/mentor/profile/picture',
  '/api/mentor/profile/status',
  '/api/mentor/profile/submit',
  '/api/mentor/requests',
  '/api/mentor/requests/:id',
  '/api/mentor/requests/:id/decline',
  '/api/mentor/requests/:id/status',
  '/api/mentor/username',
  '/api/og/mentor',
  '/api/register-mentor',
  '/api/reviews/check',
  '/api/reviews/submit',
  '/api/schedule-migration',
  '/api/username-availability',
] as const

/**
 * The type of a metric route label. Passing anything else — a `req.url`, a
 * typo, a template that was never declared — is a type error, which is what
 * makes "every route has an explicit label" checkable before the code runs.
 */
export type ApiRouteLabel = (typeof API_ROUTE_LABELS)[number]

const DECLARED: ReadonlySet<string> = new Set<string>(API_ROUTE_LABELS)

/** A UUID occupying a whole path segment — the shape of a leaked real id. */
const UUID_SEGMENT = /(^|\/)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(\/|$)/i

/**
 * Heuristics for "this label came from the request, not from the source".
 * Only used to make the thrown message specific; membership in
 * `API_ROUTE_LABELS` is the actual gate.
 */
function userDerivedReason(label: string): string | null {
  if (label.includes('?') || label.includes('#')) return 'it carries a query string or fragment'
  if (UUID_SEGMENT.test(label)) return 'it contains a UUID segment (use `:id`)'
  if (/(^|\/)\d+(\/|$)/.test(label)) return 'it contains a numeric id segment (use `:id`)'
  if (/[A-Z]/.test(label)) return 'route templates are lowercase'
  return null
}

/**
 * Reject an empty or request-derived route label in development.
 *
 * Called once per route module at import time, so in `next dev` a bad label is
 * a 500 on the first request to that route rather than a silently wrong metric.
 * Deliberately a no-op in production: the `ApiRouteLabel` type and the
 * `api-routes.test.ts` file-vs-label check already gate this before deploy, and
 * a throw at module load would take a live endpoint down for a labelling bug.
 */
export function assertApiRouteLabel(label: string): void {
  if (process.env.NODE_ENV === 'production') return

  if (typeof label !== 'string' || label.trim() === '') {
    throw new Error(
      'withObservability: route label is empty. Pass the route template as the first ' +
        "argument, e.g. withObservability('/api/mentor/requests/:id', handler)."
    )
  }

  if (DECLARED.has(label)) return

  const reason = userDerivedReason(label)
  throw new Error(
    `withObservability: route label ${JSON.stringify(label)} is not declared` +
      (reason ? ` — ${reason}` : '') +
      '. Add the route template to API_ROUTE_LABELS in src/lib/api-routes.ts. ' +
      'The label must be a literal from the source, never derived from the request.'
  )
}
