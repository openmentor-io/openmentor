package tracing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// reviewCapability stands in for a live client_requests id: whoever holds it can
// submit a review for that mentee/mentor pair, so it must not reach a span.
const reviewCapability = "11111111-2222-4333-8444-555555555555"

// TestRedactingExporterScrubsTriggerClientSpans drives the real instrumented
// client against a trigger-shaped URL: pkg/trigger appends a client_requests id
// to "…/jobs/new-request-watcher?requestId=", and otelhttp records the whole
// thing as url.full.
func TestRedactingExporterScrubsTriggerClientSpans(t *testing.T) {
	inner := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(redactingExporter{SpanExporter: inner}))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := server.URL + "/jobs/new-request-watcher?requestId=" + reviewCapability
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := httpclient.NewStandardClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	spans := inner.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no client span recorded: otelhttp was not active")
	}

	var sawURL bool
	for _, span := range spans {
		for _, attr := range span.Attributes {
			if strings.Contains(attr.Value.String(), reviewCapability) {
				t.Errorf("span attribute %s leaked the review capability: %s", attr.Key, attr.Value.String())
			}
			if attr.Key == "url.full" {
				sawURL = true
				if !strings.Contains(attr.Value.String(), "/jobs/new-request-watcher") {
					t.Errorf("url.full lost the route, redaction is too broad: %s", attr.Value.String())
				}
			}
		}
	}
	if !sawURL {
		t.Error("no url.full attribute on the client span: the redaction target moved")
	}
}

// TestInitTracerExportsRedactedSpans is the wiring half: the scrubber is useless
// unless InitTracer puts it in front of the real OTLP exporter, so this asserts
// the sentinel is absent from the bytes that leave the process.
func TestInitTracerExportsRedactedSpans(t *testing.T) {
	previousLog := logger.Log
	logger.Log = zap.NewNop()
	t.Cleanup(func() { logger.Log = previousLog })

	var mu sync.Mutex
	var exported []byte
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		exported = append(exported, body...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	previousProvider := otel.GetTracerProvider()
	shutdown, err := InitTracer("openmentor-api-test", "openmentor", "test", "test-1", "test",
		strings.TrimPrefix(collector.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { otel.SetTracerProvider(previousProvider) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := server.URL + "/jobs/request-finished?requestId=" + reviewCapability
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := httpclient.NewStandardClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	payload := string(exported)
	mu.Unlock()

	if payload == "" {
		t.Fatal("the collector received nothing: the export path changed")
	}
	if strings.Contains(payload, reviewCapability) {
		t.Error("the exported OTLP payload carries the review capability")
	}
	if !strings.Contains(payload, "/jobs/request-finished") {
		t.Error("the exported OTLP payload lost the trigger route, redaction is too broad")
	}
}

func TestRedactSpanAttributes(t *testing.T) {
	attrs := []attribute.KeyValue{
		attribute.String("url.path", "/api/v1/mentor/requests/"+reviewCapability+"/status"),
		attribute.String("url.query", "requestId="+reviewCapability+"&status=done"),
		attribute.String("url.full", "http://worker:8090/jobs/request-finished?requestId="+reviewCapability),
		attribute.String("http.route", "/api/v1/mentor/requests/:id"),
		attribute.String("session.token", "eyJhbGciOiJIUzI1NiJ9.c2VudGluZWw.s1gn4tur3"),
		attribute.Int("http.response.status_code", 200),
	}

	redacted := redactSpanAttributes(attrs)
	if redacted == nil {
		t.Fatal("redactSpanAttributes reported nothing to redact")
	}

	got := make(map[attribute.Key]string, len(redacted))
	for _, attr := range redacted {
		got[attr.Key] = attr.Value.String()
	}

	want := map[attribute.Key]string{
		"url.path":                  "/api/v1/mentor/requests/[REDACTED]/status",
		"url.query":                 "requestId=[REDACTED]&status=done",
		"url.full":                  "http://worker:8090/jobs/request-finished?requestId=[REDACTED]",
		"http.route":                "/api/v1/mentor/requests/:id",
		"session.token":             "[REDACTED]",
		"http.response.status_code": "200",
	}
	for key, expected := range want {
		if got[key] != expected {
			t.Errorf("%s = %q, want %q", key, got[key], expected)
		}
	}

	if redactSpanAttributes(redacted) != nil {
		t.Error("redacting already-clean attributes allocated a second copy")
	}
}
