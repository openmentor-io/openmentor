// faro.ts - Grafana Faro client-side observability

import {
  initializeFaro as initFaro,
  getWebInstrumentations,
  type Faro,
  type Instrumentation,
} from '@grafana/faro-web-sdk'
import { TracingInstrumentation } from '@grafana/faro-web-tracing'

let faroInstance: Faro | null = null

/** Escape a string for literal use inside a RegExp. */
function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// SECURITY (M10): magic-link/confirm tokens and review request IDs travel in
// the URL query string. Faro auto-captures page URLs, so scrub these params
// from every outgoing item before it leaves the browser.
const SENSITIVE_QUERY_PARAMS = /([?&](?:token|request_id)=)[^&#\s"']+/gi

function redactSensitive(value: unknown): unknown {
  if (typeof value === 'string') {
    return value.replace(SENSITIVE_QUERY_PARAMS, '$1[REDACTED]')
  }
  if (Array.isArray(value)) {
    return value.map(redactSensitive)
  }
  if (value && typeof value === 'object') {
    const obj = value as Record<string, unknown>
    for (const key of Object.keys(obj)) {
      obj[key] = redactSensitive(obj[key])
    }
    return obj
  }
  return value
}

/**
 * URLs that receive the W3C traceparent header on cross-origin requests.
 * Same-origin /api/* fetches get traceparent by default (no CORS list is
 * needed for them) — this list only matters for direct cross-origin calls
 * to the Go API.
 */
function buildTraceHeaderCorsUrls(): RegExp[] {
  const urls: RegExp[] = [/localhost:8081/, /backend:8081/]

  const goApiUrl = (process.env.NEXT_PUBLIC_GO_API_URL || '').trim()
  if (goApiUrl) {
    // Escape and anchor so the env value matches literally as a URL prefix.
    // (An unescaped `new RegExp('')` would match every URL.)
    urls.push(new RegExp('^' + escapeRegExp(goApiUrl)))
  }

  return urls
}

export function initializeFaro(): Faro | null {
  // Prevent double initialization and server-side execution
  if (faroInstance || typeof window === 'undefined') {
    return faroInstance
  }

  const collectorUrl = process.env.NEXT_PUBLIC_FARO_COLLECTOR_URL

  // Skip initialization if no collector URL is configured
  if (!collectorUrl) {
    // eslint-disable-next-line no-console
    console.log('[Faro] Skipping initialization - NEXT_PUBLIC_FARO_COLLECTOR_URL not configured')
    return null
  }

  const appName = process.env.NEXT_PUBLIC_FARO_APP_NAME || 'openmentor-frontend'

  const appNamespace = process.env.NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE || 'openmentor-io'

  const appVersion = process.env.NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION || '1.0.0'

  const appEnvironment = process.env.NEXT_PUBLIC_APP_ENV || process.env.NODE_ENV || 'production'

  try {
    // eslint-disable-next-line no-console
    console.log('[Faro] Initializing Grafana Faro', {
      appName,
      appVersion,
      appEnvironment,
      collectorUrl,
    })

    // Use Next.js rewrite to proxy requests and bypass CORS
    // Browser sends to /faro-collect -> Next.js rewrites to Grafana Cloud
    const proxyUrl = '/faro-collect'

    faroInstance = initFaro({
      url: proxyUrl,
      // SECURITY (M10): redact one-time tokens from URLs in every payload.
      beforeSend: (item) => {
        try {
          redactSensitive(item)
        } catch {
          // never let redaction throw away telemetry
        }
        return item
      },
      app: {
        name: appName,
        namespace: appNamespace,
        version: appVersion,
        environment: appEnvironment,
      },
      sessionTracking: {
        samplingRate: Number.parseFloat(process.env.NEXT_PUBLIC_FARO_SAMPLE_RATE || '1'),
        persistent: false,
      },
      instrumentations: [
        // Default web instrumentations: errors, console, web vitals, session
        ...getWebInstrumentations(),
        // OpenTelemetry tracing integration
        new TracingInstrumentation({
          instrumentationOptions: {
            propagateTraceHeaderCorsUrls: buildTraceHeaderCorsUrls(),
          },
        }) as Instrumentation,
      ],
    })

    // eslint-disable-next-line no-console
    console.log('[Faro] Grafana Faro initialized successfully')

    return faroInstance
  } catch (error) {
    console.error('[Faro] Failed to initialize Grafana Faro:', error)
    return null
  }
}

export function getFaro(): Faro | null {
  return faroInstance
}

/**
 * Track an SPA route change (Next Router client-side navigation).
 * Faro only captures the initial hard load by itself — this updates the
 * current view meta and emits a route_change event so client-side
 * navigations show up in Frontend Observability.
 * No-op when Faro is not initialized (collector URL unset).
 */
export function trackRouteChange(url: string): void {
  if (!faroInstance) {
    return
  }

  faroInstance.api.setView({ name: url })
  faroInstance.api.pushEvent('route_change', { url })
}

// Helper to push custom events
export function pushEvent(name: string, attributes?: Record<string, string>): void {
  if (faroInstance) {
    faroInstance.api.pushEvent(name, attributes)
  }
}

// Helper to push errors
export function pushError(error: Error, context?: Record<string, string>): void {
  if (faroInstance) {
    faroInstance.api.pushError(error, { context })
  }
}

// Helper to set user context
export function setUser(userId: string, attributes?: Record<string, string>): void {
  if (faroInstance) {
    faroInstance.api.setUser({
      id: userId,
      attributes,
    })
  }
}
