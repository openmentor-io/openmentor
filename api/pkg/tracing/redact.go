package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// redactingExporter strips capabilities out of span attributes on the way to the
// collector.
//
// It wraps the exporter instead of registering a SpanProcessor because neither
// processor hook can do this: OnEnd gets a read-only span, and OnStart runs
// BEFORE the instrumentation records its attributes — otelhttp calls
// span.SetAttributes(url.full, ...) after tracer.Start. That matters because
// pkg/trigger's worker calls put a client_requests id in the query string
// (".../jobs/new-request-watcher?requestId=<uuid>"), which is a bearer
// capability for the review flow (P14).
type redactingExporter struct {
	sdktrace.SpanExporter
}

func (e redactingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	var out []sdktrace.ReadOnlySpan
	for i, span := range spans {
		attrs := redactSpanAttributes(span.Attributes())
		if attrs == nil {
			continue
		}
		// The batch processor owns the slice it hands us, so it gets a copy.
		if out == nil {
			out = make([]sdktrace.ReadOnlySpan, len(spans))
			copy(out, spans)
		}
		out[i] = redactedSpan{ReadOnlySpan: span, attrs: attrs}
	}
	if out == nil {
		out = spans
	}
	return e.SpanExporter.ExportSpans(ctx, out)
}

// redactedSpan serves rewritten attributes for an already-ended span.
// ReadOnlySpan has an unexported method, so embedding is the only way to
// implement it outside the SDK.
type redactedSpan struct {
	sdktrace.ReadOnlySpan
	attrs []attribute.KeyValue
}

func (s redactedSpan) Attributes() []attribute.KeyValue { return s.attrs }

// redactSpanAttributes returns a rewritten copy of attrs, or nil when there was
// nothing to redact.
func redactSpanAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	var out []attribute.KeyValue
	for i, attr := range attrs {
		// A capability is always a string; a shape-matched key holding a number
		// or a bool is a derived fact (http.request.body.size).
		if attr.Value.Type() != attribute.STRING {
			continue
		}
		original := attr.Value.AsString()
		redacted := redactAttribute(attr.Key, original)
		if redacted == original {
			continue
		}
		if out == nil {
			out = make([]attribute.KeyValue, len(attrs))
			copy(out, attrs)
		}
		out[i] = attribute.String(string(attr.Key), redacted)
	}
	return out
}

func redactAttribute(key attribute.Key, value string) string {
	if redact.SensitiveKey(string(key)) {
		return redact.Placeholder
	}
	switch key {
	// A bare query string, with no leading "?" to split on.
	case "url.query":
		return redact.QueryString(value)
	// A URL or a request target: the capability can be in either half. http.route
	// is deliberately absent — it is a template, never a value.
	case "url.full", "url.path", "url.original", "http.url", "http.target":
		return redact.URL(value)
	}
	return value
}
