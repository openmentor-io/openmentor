package trigger

import (
	"context"
	"encoding/json"
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

	"github.com/openmentor-io/openmentor-api/pkg/httpclient"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

func TestMain(m *testing.M) {
	// Use a no-op logger so package-level logging doesn't panic in tests.
	logger.Log = zap.NewNop()
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
// already-cancelled parent context must not abort the trigger call (while
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
