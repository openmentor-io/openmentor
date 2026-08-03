package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// reviewCapability stands in for a live review request_id: whoever holds it can
// read the mentor's name and submit a review, so it must not reach a log line or
// a span attribute (P14).
const reviewCapability = "11111111-2222-4333-8444-555555555555"

// magicLinkToken stands in for a mentor login token arriving as a query param.
const magicLinkToken = "eyJhbGciOiJIUzI1NiJ9.c2VudGluZWw.s1gn4tur3"

func init() { metrics.Init("openmentor-middleware-test") }

// observeLogs swaps the global logger for a recording one.
func observeLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Log
	logger.Log = zap.New(core)
	t.Cleanup(func() { logger.Log = previous })

	return logs
}

// recordSpans installs a recording tracer provider for the duration of a test.
func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return exporter
}

// reviewRouter mirrors the real middleware order (otelgin starts the span, the
// observability middleware logs and redacts afterwards).
func reviewRouter(status int) *gin.Engine {
	r := gin.New()
	r.Use(otelgin.Middleware("openmentor-api-test"))
	r.Use(ObservabilityMiddleware())
	r.GET("/api/v1/reviews/:requestId/check", func(c *gin.Context) {
		c.Status(status)
	})
	return r
}

func TestObservabilityKeepsReviewCapabilityOutOfLogsAndSpans(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			logs := observeLogs(t)
			spans := recordSpans(t)

			target := "/api/v1/reviews/" + reviewCapability + "/check" +
				"?request_id=" + reviewCapability +
				"&login_token=" + magicLinkToken +
				"&status=done"
			w := httptest.NewRecorder()
			reviewRouter(status).ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, http.NoBody))

			if w.Code != status {
				t.Fatalf("status = %d, want %d", w.Code, status)
			}

			entries := logs.All()
			if len(entries) == 0 {
				t.Fatal("no log entry recorded: the middleware did not log the request")
			}
			for _, entry := range entries {
				rendered := entry.Message + " " + fmt.Sprint(entry.ContextMap())
				assertNoSecrets(t, "log entry", rendered)
				if !strings.Contains(rendered, redact.Placeholder) && status >= 400 {
					t.Errorf("log entry does not show anything was redacted: %s", rendered)
				}
			}

			recorded := spans.GetSpans()
			if len(recorded) == 0 {
				t.Fatal("no span recorded: otelgin was not active")
			}
			var sawPath, sawQuery bool
			for _, span := range recorded {
				for _, attr := range span.Attributes {
					assertNoSecrets(t, string(attr.Key), attr.Value.String())
					switch attr.Key {
					case "url.path":
						sawPath = true
					case "url.query":
						sawQuery = true
					}
				}
			}
			if !sawPath {
				t.Error("no url.path attribute on the span: the redaction target moved")
			}
			if !sawQuery {
				t.Error("no url.query attribute on the span: the redacted query was not written")
			}
		})
	}
}

func TestObservabilityKeepsNonSensitiveParamsReadable(t *testing.T) {
	logs := observeLogs(t)

	r := gin.New()
	r.Use(ObservabilityMiddleware())
	r.GET("/api/v1/mentor/requests/:id", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/mentor/requests/m-42?group=new", http.NoBody))

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("recorded %d log entries, want 1", len(entries))
	}
	rendered := fmt.Sprint(entries[0].ContextMap())
	for _, want := range []string{"m-42", "new"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("log entry lost %q, redaction is too broad: %s", want, rendered)
		}
	}
}

func assertNoSecrets(t *testing.T, where, value string) {
	t.Helper()
	for name, secret := range map[string]string{
		"review capability": reviewCapability,
		"magic-link token":  magicLinkToken,
	} {
		if strings.Contains(value, secret) {
			t.Errorf("%s leaked the %s: %s", where, name, value)
		}
	}
}
