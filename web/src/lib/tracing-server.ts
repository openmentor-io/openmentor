// tracing-server.ts - OpenTelemetry server-side (Next.js SSR/API routes) tracing

import { NodeSDK } from '@opentelemetry/sdk-node'
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http'
import { getNodeAutoInstrumentations } from '@opentelemetry/auto-instrumentations-node'
import { resourceFromAttributes } from '@opentelemetry/resources'
import { RedactingSpanExporter, shouldIgnoreIncomingRequest } from './tracing-redact'
// OpenTelemetry semantic convention attribute keys
const ATTR_SERVICE_NAME = 'service.name'
const ATTR_SERVICE_VERSION = 'service.version'
const ATTR_SERVICE_NAMESPACE = 'service.namespace'
const ATTR_SERVICE_INSTANCE_ID = 'service.instance.id'
const ATTR_DEPLOYMENT_ENVIRONMENT = 'deployment.environment.name'
import { v4 as uuidv4 } from 'uuid'

let sdk: NodeSDK | undefined

function registerServerTracing(): void {
  // Prevent double initialization
  if (sdk) {
    return
  }

  const alloyEndpoint = process.env.O11Y_EXPORTER_ENDPOINT || 'http://alloy:4318'
  const serviceName = process.env.O11Y_FE_SERVICE_NAME || 'openmentor-frontend'
  const serviceNamespace = process.env.O11Y_SERVICE_NAMESPACE || 'openmentor-io'
  const serviceVersion = process.env.O11Y_FE_SERVICE_VERSION || '1.0.0'
  const environment = process.env.APP_ENV || process.env.NODE_ENV || 'production'

  // Ensure endpoint has protocol (JavaScript OTLP exporter needs full URL)
  // Backend uses Go client which expects host:port, frontend needs http://host:port
  const exporterUrl = alloyEndpoint.startsWith('http')
    ? `${alloyEndpoint}/v1/traces`
    : `http://${alloyEndpoint}/v1/traces`

  // Create OTLP exporter instance, wrapped so no span leaves the process with a
  // login token or a review request_id in a url.* attribute (P14).
  const traceExporter = new RedactingSpanExporter(
    new OTLPTraceExporter({
      url: exporterUrl,
      headers: {},
    })
  )

  // Get or generate service instance ID
  const instanceId = process.env.SERVICE_INSTANCE_ID || uuidv4()

  // Create resource with service information. OTel 2.x replaced the
  // `new Resource(...)` constructor with this factory.
  const resource = resourceFromAttributes({
    [ATTR_SERVICE_NAME]: serviceName,
    [ATTR_SERVICE_VERSION]: serviceVersion,
    [ATTR_SERVICE_NAMESPACE]: serviceNamespace,
    [ATTR_SERVICE_INSTANCE_ID]: instanceId,
    [ATTR_DEPLOYMENT_ENVIRONMENT]: environment,
  })

  // Initialize Node.js SDK with automatic instrumentation
  const sdkConfig = {
    traceExporter,
    resource,
    instrumentations: [
      getNodeAutoInstrumentations({
        '@opentelemetry/instrumentation-http': {
          requireParentforOutgoingSpans: false,
          requireParentforIncomingSpans: false,
          // The auth-callback pages exist only to spend a one-time login token,
          // so their spans are built entirely from a credential-bearing URL and
          // are dropped rather than scrubbed (P14).
          ignoreIncomingRequestHook: (request: { url?: string }): boolean =>
            shouldIgnoreIncomingRequest(request.url),
        },
        '@opentelemetry/instrumentation-express': {},
        '@opentelemetry/instrumentation-fs': { enabled: false },
        '@opentelemetry/instrumentation-dns': { enabled: false },
        '@opentelemetry/instrumentation-net': { enabled: false },
        '@opentelemetry/instrumentation-undici': {},
      }),
    ],
  } as unknown as ConstructorParameters<typeof NodeSDK>[0]

  sdk = new NodeSDK(sdkConfig)

  // Start the SDK
  sdk.start()

  // eslint-disable-next-line no-console
  console.log('[Tracing] Server-side OpenTelemetry initialized', {
    serviceName,
    serviceNamespace,
    serviceVersion,
    environment,
    exporterUrl,
  })

  // Note: No graceful shutdown handler - SDK will export periodically
  // Accepting potential data loss on termination for code simplicity
}

export { registerServerTracing }
