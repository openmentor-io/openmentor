package middleware

import (
	"errors"
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

// TestObservabilityRedactsCapabilityUnderAnInnocentParamName covers the mentor
// request routes: their :id is the SAME client_requests.id that
// /api/v1/reviews/:requestId accepts as authorization, so matching on the
// parameter's name would let the capability through here.
func TestObservabilityRedactsCapabilityUnderAnInnocentParamName(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			logs := observeLogs(t)
			spans := recordSpans(t)

			r := gin.New()
			r.Use(otelgin.Middleware("openmentor-api-test"))
			r.Use(ObservabilityMiddleware())
			r.POST("/api/v1/mentor/requests/:id/status", func(c *gin.Context) { c.Status(status) })

			w := httptest.NewRecorder()
			target := "/api/v1/mentor/requests/" + reviewCapability + "/status"
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, target, http.NoBody))

			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("recorded %d log entries, want 1", len(entries))
			}
			rendered := fmt.Sprint(entries[0].ContextMap())
			assertNoSecrets(t, "log entry", rendered)
			if !strings.Contains(rendered, "/api/v1/mentor/requests/") {
				t.Errorf("log entry lost the route, redaction is too broad: %s", rendered)
			}
			// Correlation survives the redaction on the lines that need it.
			if status >= 400 && !strings.Contains(rendered, redact.ID(reviewCapability)) {
				t.Errorf("route_params carries no hashed reference: %s", rendered)
			}

			recorded := spans.GetSpans()
			if len(recorded) == 0 {
				t.Fatal("no span recorded: otelgin was not active")
			}
			for _, span := range recorded {
				for _, attr := range span.Attributes {
					assertNoSecrets(t, string(attr.Key), attr.Value.String())
				}
			}
		})
	}
}

// TestObservabilityRedactsAttachedErrorText covers the sink that the path and
// route_params rules do not reach: handlers attach the reason for a 4xx with the
// row id inside the message (`failed to fetch request id="<uuid>"`), and the
// middleware logs that message verbatim — so a routine not-found or ownership
// mismatch logged the capability next to the redacted path.
func TestObservabilityRedactsAttachedErrorText(t *testing.T) {
	logs := observeLogs(t)

	r := gin.New()
	r.Use(ObservabilityMiddleware())
	r.GET("/api/v1/mentor/requests/:id", func(c *gin.Context) {
		// Verbatim shape from MentorRequestsHandler.GetRequestByID.
		_ = c.Error(fmt.Errorf("failed to fetch request id=%q: %w", reviewCapability, errRequestNotFound)) //nolint:errcheck
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/mentor/requests/"+reviewCapability, http.NoBody))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("recorded %d log entries, want 1", len(entries))
	}
	rendered := fmt.Sprint(entries[0].ContextMap())
	assertNoSecrets(t, "log entry", rendered)
	if !strings.Contains(rendered, "failed to fetch request") {
		t.Errorf("log entry lost the reason, redaction is too broad: %s", rendered)
	}
	// The reason still points at a row: the hashed ref matches route_params.
	if !strings.Contains(rendered, redact.ID(reviewCapability)) {
		t.Errorf("error text carries no hashed reference: %s", rendered)
	}
}

var errRequestNotFound = errors.New("request not found")

// TestObservabilityRedactsAttachedErrorOnTheSpan covers the two sinks for the
// same attached message that redacting only the log field leaves open: otelgin
// copies c.Errors.String() into the span status and calls RecordError, which
// repeats the text as an exception event. Both are exported even though the log
// line beside them is clean, so the value itself has to be redacted.
func TestObservabilityRedactsAttachedErrorOnTheSpan(t *testing.T) {
	_ = observeLogs(t)
	spans := recordSpans(t)

	r := gin.New()
	r.Use(otelgin.Middleware("openmentor-api-test"))
	r.Use(ObservabilityMiddleware())
	r.GET("/api/v1/mentor/requests/:id", func(c *gin.Context) {
		_ = c.Error(fmt.Errorf("failed to fetch request id=%q: %w", reviewCapability, errRequestNotFound)) //nolint:errcheck
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/mentor/requests/"+reviewCapability, http.NoBody))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	recorded := spans.GetSpans()
	if len(recorded) == 0 {
		t.Fatal("no span recorded: otelgin was not active")
	}
	var sawStatus, sawException bool
	for _, span := range recorded {
		if span.Status.Description != "" {
			sawStatus = true
			assertNoSecrets(t, "span status", span.Status.Description)
		}
		for _, event := range span.Events {
			for _, attr := range event.Attributes {
				if event.Name == "exception" {
					sawException = true
				}
				assertNoSecrets(t, "span event "+event.Name+" "+string(attr.Key), attr.Value.String())
			}
		}
	}
	if !sawStatus {
		t.Error("no span status description: otelgin stopped copying c.Errors, redaction target moved")
	}
	if !sawException {
		t.Error("no exception event: otelgin stopped recording c.Errors, redaction target moved")
	}
}

// TestRecoveryKeepsCapabilityOutOfPanicLogsAndSpans pins the panic path. A panic
// unwinds out of c.Next() before the observability middleware redacts anything,
// so the outermost RecoveryMiddleware used to log the raw request path, and
// otelgin's own deferred span.End() exported the raw url.path it tagged at span
// start. The middleware order here mirrors cmd/api/main.go.
func TestRecoveryKeepsCapabilityOutOfPanicLogsAndSpans(t *testing.T) {
	logs := observeLogs(t)
	spans := recordSpans(t)

	r := gin.New()
	r.Use(RecoveryMiddleware())
	r.Use(otelgin.Middleware("openmentor-api-test"))
	r.Use(ObservabilityMiddleware())
	r.POST("/api/v1/reviews/:requestId", func(c *gin.Context) {
		panic("nil map write in review submission")
	})

	w := httptest.NewRecorder()
	target := "/api/v1/reviews/" + reviewCapability + "?login_token=" + magicLinkToken
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, target, http.NoBody))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	entries := logs.All()
	if len(entries) == 0 {
		t.Fatal("no log entry recorded: the panic was not logged")
	}
	var sawPanic bool
	for _, entry := range entries {
		rendered := entry.Message + " " + fmt.Sprint(entry.ContextMap())
		assertNoSecrets(t, "log entry", rendered)
		if entry.Message == "panic recovered" {
			sawPanic = true
			// The route still has to be readable, or the line is not actionable.
			if !strings.Contains(rendered, "/api/v1/reviews/"+redact.Placeholder) {
				t.Errorf("panic log lost the route: %s", rendered)
			}
		}
	}
	if !sawPanic {
		t.Error("no \"panic recovered\" entry: RecoveryMiddleware did not run")
	}

	recorded := spans.GetSpans()
	if len(recorded) == 0 {
		t.Fatal("no span recorded: otelgin was not active")
	}
	var sawPath bool
	for _, span := range recorded {
		for _, attr := range span.Attributes {
			assertNoSecrets(t, string(attr.Key), attr.Value.String())
			if attr.Key == "url.path" {
				sawPath = true
				if attr.Value.AsString() != "/api/v1/reviews/"+redact.Placeholder {
					t.Errorf("url.path = %q, want the redacted path", attr.Value.AsString())
				}
			}
		}
	}
	if !sawPath {
		t.Error("no url.path attribute on the span: the redaction target moved")
	}
}

func TestObservabilityKeepsNonSensitiveParamsReadable(t *testing.T) {
	logs := observeLogs(t)

	r := gin.New()
	r.Use(ObservabilityMiddleware())
	r.GET("/api/v1/mentor/requests/:id", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	// "m-42" is deliberately not UUID-shaped: the shape is what marks a value as
	// a capability, so a legacy id has to stay readable.
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
