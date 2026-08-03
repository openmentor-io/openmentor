package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// observedLogs collects everything the package logs during the test run.
//
// It is installed ONCE, before any test starts, rather than swapped in by the
// test that needs it: CallAsync logs from a detached goroutine that outlives the
// test which started it, so assigning logger.Log mid-run is a data race against
// an earlier test's still-in-flight trigger. observer's core is safe for
// concurrent writes, so one install covers every test.
var observedLogs *observer.ObservedLogs

func TestMain(m *testing.M) {
	var core zapcore.Core
	core, observedLogs = observer.New(zap.DebugLevel)
	// Observed instead of no-op: still discards output, but lets a test read back
	// what was logged. Package-level logging must not panic either way.
	logger.Log = zap.New(core)
	os.Exit(m.Run())
}

// receivedRequest captures what the fake worker endpoint saw.
type receivedRequest struct {
	method      string
	url         string
	workerToken string
	contentType string
	traceparent string
	body        []byte
}

// newCaptureServer starts an httptest server that pushes every request it
// receives onto the returned channel. The trigger calls run in goroutines,
// so tests synchronize by reading from the channel.
func newCaptureServer(t *testing.T) (*httptest.Server, <-chan receivedRequest) {
	t.Helper()
	requests := make(chan receivedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- receivedRequest{
			method:      r.Method,
			url:         r.URL.String(),
			workerToken: r.Header.Get(WorkerTokenHeader),
			contentType: r.Header.Get("Content-Type"),
			traceparent: r.Header.Get("traceparent"),
			body:        body,
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

// waitForRequest waits for the async trigger call to hit the fake endpoint.
func waitForRequest(t *testing.T, requests <-chan receivedRequest) receivedRequest {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("trigger call never reached the endpoint")
		return receivedRequest{}
	}
}

func TestCallAsyncAppendsRecordIDAndSendsToken(t *testing.T) {
	srv, requests := newCaptureServer(t)

	CallAsync(context.Background(), srv.URL+"/jobs/new-mentor-watcher?mentorId=", "rec123", "secret-token", httpclient.NewStandardClient())

	req := waitForRequest(t, requests)
	if req.method != http.MethodGet {
		t.Errorf("method = %s, want GET", req.method)
	}
	if req.url != "/jobs/new-mentor-watcher?mentorId=rec123" {
		t.Errorf("url = %s, want /jobs/new-mentor-watcher?mentorId=rec123", req.url)
	}
	if req.workerToken != "secret-token" {
		t.Errorf("%s header = %q, want %q", WorkerTokenHeader, req.workerToken, "secret-token")
	}
}

func TestCallAsyncOmitsTokenHeaderWhenUnset(t *testing.T) {
	srv, requests := newCaptureServer(t)

	CallAsync(context.Background(), srv.URL+"/jobs/process-mentee-review?reviewId=", "rev1", "", httpclient.NewStandardClient())

	req := waitForRequest(t, requests)
	if req.workerToken != "" {
		t.Errorf("%s header = %q, want empty (no token configured)", WorkerTokenHeader, req.workerToken)
	}
}

func TestCallAsyncWithPayloadPostsJSONAndSendsToken(t *testing.T) {
	srv, requests := newCaptureServer(t)

	payload := map[string]string{"email": "mentor@example.com", "authUrl": "https://openmentor.io/auth"}
	CallAsyncWithPayload(context.Background(), srv.URL+"/jobs/mentor-login-email", payload, "secret-token", httpclient.NewStandardClient())

	req := waitForRequest(t, requests)
	if req.method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.method)
	}
	if req.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", req.contentType)
	}
	if req.workerToken != "secret-token" {
		t.Errorf("%s header = %q, want %q", WorkerTokenHeader, req.workerToken, "secret-token")
	}
	var got map[string]string
	if err := json.Unmarshal(req.body, &got); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if got["email"] != payload["email"] || got["authUrl"] != payload["authUrl"] {
		t.Errorf("body = %v, want %v", got, payload)
	}
}

func TestCallAsyncWithPayloadOmitsTokenHeaderWhenUnset(t *testing.T) {
	srv, requests := newCaptureServer(t)

	CallAsyncWithPayload(context.Background(), srv.URL+"/jobs/moderator-login-email", map[string]string{"k": "v"}, "", httpclient.NewStandardClient())

	req := waitForRequest(t, requests)
	if req.workerToken != "" {
		t.Errorf("%s header = %q, want empty (no token configured)", WorkerTokenHeader, req.workerToken)
	}
}

func TestCallAsyncSkipsWhenNoURLConfigured(t *testing.T) {
	srv, requests := newCaptureServer(t)
	_ = srv

	CallAsync(context.Background(), "", "rec123", "secret-token", httpclient.NewStandardClient())
	CallAsyncWithPayload(context.Background(), "", map[string]string{"k": "v"}, "secret-token", httpclient.NewStandardClient())

	select {
	case req := <-requests:
		t.Fatalf("unexpected request with empty trigger URL: %+v", req)
	case <-time.After(100 * time.Millisecond):
		// No call was made, as expected.
	}
}

// TestCallAsyncTransportFailureKeepsCapabilityOutOfLogs drives the failure path
// for real: net/http returns a *url.Error whose Error() renders the whole target
// URL, so the logged error text is a sink of its own — the sanitized url and
// record_ref fields do not protect it.
func TestCallAsyncTransportFailureKeepsCapabilityOutOfLogs(t *testing.T) {
	// A live client_requests id: whoever holds it can submit a review as that
	// mentee, so it must not reach a log line (P14).
	const capability = "11111111-2222-4333-8444-555555555555"

	observedLogs.TakeAll() // drop what the earlier tests logged

	// Port 1 refuses instantly, so this is a genuine transport error and not a
	// stub: err is the *url.Error net/http really produces.
	CallAsync(context.Background(),
		"http://127.0.0.1:1/jobs/new-request-watcher?requestId=", capability, "", httpclient.NewStandardClient())

	deadline := time.Now().Add(5 * time.Second)
	for observedLogs.FilterMessage("Failed to call trigger URL").Len() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the trigger call never failed: the test drove nothing")
		}
		time.Sleep(10 * time.Millisecond)
	}

	entries := observedLogs.All()
	for _, entry := range entries {
		rendered := entry.Message + " " + fmt.Sprint(entry.ContextMap())
		if strings.Contains(rendered, capability) {
			t.Errorf("log entry leaked the capability: %s", rendered)
		}
	}

	// The failure is still diagnosable: the reason survives, only the id goes.
	failure := observedLogs.FilterMessage("Failed to call trigger URL").All()[0]
	errText, _ := failure.ContextMap()["error"].(string)
	if !strings.Contains(errText, "connection refused") {
		t.Errorf("error field = %q, want it to keep the transport reason", errText)
	}
	if !strings.Contains(errText, "requestId="+redact.Placeholder) {
		t.Errorf("error field = %q, want the capability replaced in place", errText)
	}
}

// withTestTraceContext installs an SDK tracer provider and the W3C
// propagator globally (restored on cleanup) and returns a context carrying
// a live span, mimicking a Gin request context inside a traced handler.
func withTestTraceContext(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()

	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	ctx, span := tp.Tracer("trigger-test").Start(context.Background(), "parent")
	t.Cleanup(func() { span.End() })
	return ctx, span.SpanContext()
}

// requireTraceparentFor asserts the captured request carried a W3C
// traceparent header belonging to the caller's trace.
func requireTraceparentFor(t *testing.T, req receivedRequest, parent trace.SpanContext) {
	t.Helper()
	if req.traceparent == "" {
		t.Fatal("traceparent header missing: trace context not propagated")
	}
	if !strings.Contains(req.traceparent, parent.TraceID().String()) {
		t.Errorf("traceparent = %q does not contain caller trace id %s",
			req.traceparent, parent.TraceID())
	}
}

func TestCallAsyncInjectsTraceparent(t *testing.T) {
	srv, requests := newCaptureServer(t)
	ctx, parent := withTestTraceContext(t)

	CallAsync(ctx, srv.URL+"/jobs/new-mentor-watcher?mentorId=", "rec123", "secret-token", httpclient.NewStandardClient())

	requireTraceparentFor(t, waitForRequest(t, requests), parent)
}

func TestCallAsyncWithPayloadInjectsTraceparent(t *testing.T) {
	srv, requests := newCaptureServer(t)
	ctx, parent := withTestTraceContext(t)

	CallAsyncWithPayload(ctx, srv.URL+"/jobs/mentor-login-email", map[string]string{"k": "v"}, "secret-token", httpclient.NewStandardClient())

	requireTraceparentFor(t, waitForRequest(t, requests), parent)
}

// TestCallAsyncSurvivesCallerCancellation pins the context.WithoutCancel
// behavior: the trigger goroutine outlives the caller's HTTP request, so an
// already-canceled parent context must not abort the trigger call (while
// its values, e.g. trace context, still propagate).
func TestCallAsyncSurvivesCallerCancellation(t *testing.T) {
	srv, requests := newCaptureServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the caller's request is already gone

	CallAsync(ctx, srv.URL+"/jobs/new-mentor-watcher?mentorId=", "rec123", "", httpclient.NewStandardClient())
	if req := waitForRequest(t, requests); req.url != "/jobs/new-mentor-watcher?mentorId=rec123" {
		t.Errorf("url = %s, want /jobs/new-mentor-watcher?mentorId=rec123", req.url)
	}

	CallAsyncWithPayload(ctx, srv.URL+"/jobs/mentor-login-email", map[string]string{"k": "v"}, "", httpclient.NewStandardClient())
	if req := waitForRequest(t, requests); req.method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.method)
	}
}
