package worker

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// installTestTracerProvider swaps in a recording tracer provider (and the
// W3C propagator) for the duration of a test and returns the exporter that
// captures every ended span.
func installTestTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	return exporter
}

// TestServerCreatesHTTPServerSpans is the otelgin smoke test: a request to
// the worker HTTP server must produce a server span.
func TestServerCreatesHTTPServerSpans(t *testing.T) {
	exporter := installTestTracerProvider(t)

	server := NewServer(testConfig(), nil)
	w := performRequest(server.Engine(), http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", w.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded: otelgin middleware not active")
	}
	if got := spans[0].SpanKind; got != trace.SpanKindServer {
		t.Errorf("span kind = %v, want server", got)
	}
}

// TestServerExtractsTraceparent verifies the API->worker propagation half on
// the worker side: an incoming W3C traceparent header must become the parent
// of the otelgin server span, joining the API's trace.
func TestServerExtractsTraceparent(t *testing.T) {
	exporter := installTestTracerProvider(t)

	server := NewServer(testConfig(), nil)

	const (
		traceID  = "4bf92f3577b34da6a3ce929d0e0e4736"
		parentID = "00f067aa0ba902b7"
	)
	w := performRequest(server.Engine(), http.MethodGet, "/healthz", map[string]string{
		"traceparent": "00-" + traceID + "-" + parentID + "-01",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", w.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}
	span := spans[0]
	if got := span.SpanContext.TraceID().String(); got != traceID {
		t.Errorf("trace id = %s, want %s (traceparent not extracted)", got, traceID)
	}
	if got := span.Parent.SpanID().String(); got != parentID {
		t.Errorf("parent span id = %s, want %s", got, parentID)
	}
}

// findAttr returns the string form of an attribute on a finished span.
func findAttr(span tracetest.SpanStub, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range span.Attributes {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// TestRunCronJobCreatesRootSpan verifies a scheduler-style run (background
// context) produces a root span named cron.<job> with outcome attributes.
func TestRunCronJobCreatesRootSpan(t *testing.T) {
	exporter := installTestTracerProvider(t)

	_, err := runCronJob(context.Background(), "span-job", func(ctx context.Context) (JobSummary, error) {
		if !trace.SpanFromContext(ctx).SpanContext().IsValid() {
			t.Error("job did not receive the span context")
		}
		return JobSummary{MentorsMatched: 2}, nil
	})
	if err != nil {
		t.Fatalf("runCronJob returned error: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "cron.span-job" {
		t.Errorf("span name = %q, want cron.span-job", span.Name)
	}
	if span.Parent.IsValid() {
		t.Errorf("scheduler run should be a trace root, got parent %s", span.Parent.SpanID())
	}
	if got, ok := findAttr(span, "cron.outcome"); !ok || got.AsString() != "success" {
		t.Errorf("cron.outcome = %v (present=%v), want success", got.AsString(), ok)
	}
	if _, ok := findAttr(span, "cron.duration_seconds"); !ok {
		t.Error("cron.duration_seconds attribute missing")
	}
	if span.Status.Code == codes.Error {
		t.Error("successful run must not set error status")
	}
}

// TestRunCronJobSpanErrorStatus verifies a failing job marks the span as an
// error and records the outcome.
func TestRunCronJobSpanErrorStatus(t *testing.T) {
	exporter := installTestTracerProvider(t)

	jobErr := errors.New("boom")
	if _, err := runCronJob(context.Background(), "failing-job", func(ctx context.Context) (JobSummary, error) {
		return JobSummary{}, jobErr
	}); !errors.Is(err, jobErr) {
		t.Fatalf("runCronJob err = %v, want %v", err, jobErr)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status.Code)
	}
	if got, ok := findAttr(span, "cron.outcome"); !ok || got.AsString() != "error" {
		t.Errorf("cron.outcome = %v, want error", got.AsString())
	}
	if len(span.Events) == 0 {
		t.Error("expected a recorded error event on the span")
	}
}

// TestRunCronJobSpanPanicStatus verifies a panicking job still ends the span
// with error status and the panic outcome.
func TestRunCronJobSpanPanicStatus(t *testing.T) {
	exporter := installTestTracerProvider(t)

	if _, err := runCronJob(context.Background(), "panicking-job", func(ctx context.Context) (JobSummary, error) {
		panic("kaboom")
	}); err == nil {
		t.Fatal("runCronJob should return an error on panic")
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}
	span := spans[0]
	if span.Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status.Code)
	}
	if got, ok := findAttr(span, "cron.outcome"); !ok || got.AsString() != "panic" {
		t.Errorf("cron.outcome = %v, want panic", got.AsString())
	}
}

// TestManualCronTriggerNestsUnderRequestSpan verifies composition: a manual
// POST /jobs/cron/<name> run's cron span is a child of the otelgin server
// span because runCronJob receives the Gin request context.
func TestManualCronTriggerNestsUnderRequestSpan(t *testing.T) {
	exporter := installTestTracerProvider(t)

	env := newJobsTestEnv()

	w := performRequest(env.server.Engine(), http.MethodPost, "/jobs/cron/randomize-sort-order", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("manual trigger status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	spans := exporter.GetSpans()
	var cronSpan, serverSpan *tracetest.SpanStub
	for i := range spans {
		switch {
		case spans[i].Name == "cron.randomize-sort-order":
			cronSpan = &spans[i]
		case spans[i].SpanKind == trace.SpanKindServer:
			serverSpan = &spans[i]
		}
	}
	if cronSpan == nil || serverSpan == nil {
		t.Fatalf("missing spans: cron=%v server=%v", cronSpan != nil, serverSpan != nil)
	}
	if cronSpan.Parent.SpanID() != serverSpan.SpanContext.SpanID() {
		t.Errorf("cron span parent = %s, want otelgin server span %s",
			cronSpan.Parent.SpanID(), serverSpan.SpanContext.SpanID())
	}
	if cronSpan.SpanContext.TraceID() != serverSpan.SpanContext.TraceID() {
		t.Error("cron span not in the same trace as the request span")
	}
}
