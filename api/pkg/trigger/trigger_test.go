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

// testLogs collects every line this package's code logs. One observer for the
// whole binary, installed here — before any test, so before any detached trigger
// goroutine exists. Swapping logger.Log per test instead races with the
// goroutines earlier tests left running, since those keep reading the global.
var testLogs *observer.ObservedLogs

func TestMain(m *testing.M) {
	var core zapcore.Core
	core, testLogs = observer.New(zap.DebugLevel)
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

// recordCapability is a live client_requests id. Two of the three record-id
// triggers append one, and the review flow accepts it as a bearer credential
// (DECISIONS D45), so it must not reach a log line.
const recordCapability = "11111111-2222-4333-8444-555555555555"

// awaitEntry polls testLogs until an entry whose message contains want appears,
// because the work runs on a detached goroutine.
func awaitEntry(t *testing.T, want string) observer.LoggedEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range testLogs.All() {
			if strings.Contains(entry.Message, want) {
				return entry
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no log entry matching %q within the timeout", want)
	return observer.LoggedEntry{}
}

// TestCallAsyncLogsNoRawRecordID: the record id is appended to the request URL
// but only ever logged as a hashed reference, so neither it nor the assembled
// target URL may appear in any field. The `url` field is the trigger's BASE URL
// on purpose — dropping it entirely would leave the operator unable to tell
// which trigger a line is about.
func TestCallAsyncLogsNoRawRecordID(t *testing.T) {
	srv, requests := newCaptureServer(t)
	base := srv.URL + "/jobs/new-request-watcher?requestId="
	wantRef := redact.ID(recordCapability)

	CallAsync(context.Background(), base, recordCapability, "secret-token", httpclient.NewStandardClient())
	waitForRequest(t, requests)
	awaitEntry(t, "Trigger URL called successfully")

	// recordCapability is used by no other test, so a leak anywhere in the
	// shared log is this call's.
	ours := 0
	for _, entry := range testLogs.All() {
		fields := entry.ContextMap()
		if rendered := entry.Message + " " + fmt.Sprint(fields); strings.Contains(rendered, recordCapability) {
			t.Errorf("log entry leaked the raw record id: %s", rendered)
		}
		if ref, _ := fields["record_ref"].(string); ref != wantRef {
			continue // another test's entry
		}
		ours++
		if url, _ := fields["url"].(string); url != base {
			t.Errorf("url field = %q, want the base trigger URL %q", url, base)
		}
	}
	if ours == 0 {
		t.Errorf("no entry carried record_ref = %q, so lines about one record cannot be correlated", wantRef)
	}
}

// panickingClient stands in for a dependency that panics mid-call.
type panickingClient struct{ called chan struct{} }

func (c *panickingClient) Do(*http.Request) (*http.Response, error) {
	close(c.called)
	panic("boom from the trigger's http client")
}
func (c *panickingClient) Get(string) (*http.Response, error) { return nil, nil }
func (c *panickingClient) Post(string, string, io.Reader) (*http.Response, error) {
	return nil, nil
}

// TestCallAsyncRecoversPanic: both calls dispatch a DETACHED goroutine, where
// recover() is the only thing between a panicking dependency and the process
// dying with every in-flight request — RecoveryMiddleware cannot see it. The
// test reaching its assertions at all is the proof.
func TestCallAsyncRecoversPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(httpclient.Client)
	}{
		{"CallAsync", func(c httpclient.Client) {
			CallAsync(context.Background(), "http://worker/jobs/x?id=", recordCapability, "t", c)
		}},
		{"CallAsyncWithPayload", func(c httpclient.Client) {
			CallAsyncWithPayload(context.Background(), "http://worker/jobs/y", map[string]string{"k": "v"}, "t", c)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &panickingClient{called: make(chan struct{})}

			tc.call(client)

			select {
			case <-client.called:
			case <-time.After(5 * time.Second):
				t.Fatal("the trigger goroutine never reached the http client")
			}
			entry := awaitEntry(t, "Recovered panic in background task")
			if task, _ := entry.ContextMap()["task"].(string); !strings.HasPrefix(task, "trigger_") {
				t.Errorf("recovery entry task = %q, want a trigger_* task so the log says what panicked", task)
			}
		})
	}
}
