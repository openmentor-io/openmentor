/**
 * Environment variable type declarations
 */

declare namespace NodeJS {
  interface ProcessEnv {
    // Go API
    NEXT_PUBLIC_GO_API_URL?: string
    GO_API_INTERNAL_TOKEN?: string

    // S3-compatible object storage (profile images)
    NEXT_PUBLIC_S3_STORAGE_ENDPOINT?: string
    NEXT_PUBLIC_S3_STORAGE_BUCKET?: string

    // Optional CDN in front of the storage bucket
    NEXT_PUBLIC_CDN_ENDPOINT?: string

    // Cloudflare Turnstile (verification happens in the Go API)
    NEXT_PUBLIC_TURNSTILE_SITE_KEY?: string

    // Auth tokens
    METRICS_AUTH_TOKEN?: string

    // Logging
    LOG_LEVEL?: 'debug' | 'info' | 'warn' | 'error'
    LOG_DIR?: string

    // Server-side OpenTelemetry tracing
    O11Y_EXPORTER_ENDPOINT?: string
    O11Y_FE_SERVICE_NAME?: string
    O11Y_SERVICE_NAMESPACE?: string
    O11Y_FE_SERVICE_VERSION?: string
    APP_ENV?: string
    SERVICE_INSTANCE_ID?: string
    HOSTNAME?: string

    // Client-side service identity
    NEXT_PUBLIC_O11Y_SERVICE_NAMESPACE?: string
    NEXT_PUBLIC_O11Y_FE_SERVICE_VERSION?: string
    NEXT_PUBLIC_APP_ENV?: string

    // Grafana Faro (client-side observability)
    NEXT_PUBLIC_FARO_COLLECTOR_URL?: string
    NEXT_PUBLIC_FARO_APP_NAME?: string
    NEXT_PUBLIC_FARO_SAMPLE_RATE?: string

    // Analytics
    NEXT_PUBLIC_ANALYTICS_PROVIDER?: string
    NEXT_PUBLIC_ANALYTICS_EVENT_VERSION?: string

    // PostHog
    NEXT_PUBLIC_POSTHOG_KEY?: string
    NEXT_PUBLIC_POSTHOG_HOST?: string
    POSTHOG_PERSONAL_API_KEY?: string
    POSTHOG_PROJECT_ID?: string

    // Domain
    DOMAIN?: string

    // Node
    NODE_ENV?: 'development' | 'production' | 'test'
  }
}
