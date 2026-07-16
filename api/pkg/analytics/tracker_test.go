package analytics

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

func TestMain(m *testing.M) {
	// resolveProvider logs via the global logger on misconfiguration paths;
	// initialize it so those tests don't hit a nil logger.
	if err := logger.Initialize(logger.Config{Level: "error", Environment: "development"}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

type capturedRequest struct {
	URL  string
	Body string
}

type captureTransport struct {
	mu       sync.Mutex
	requests []capturedRequest
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.requests = append(t.requests, capturedRequest{
		URL:  req.URL.String(),
		Body: string(body),
	})
	t.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":1}`)),
		Header:     make(http.Header),
	}, nil
}

func (t *captureTransport) Requests() []capturedRequest {
	t.mu.Lock()
	defer t.mu.Unlock()

	cloned := make([]capturedRequest, len(t.requests))
	copy(cloned, t.requests)
	return cloned
}

type slowTransport struct {
	mu    sync.Mutex
	delay time.Duration
	calls int
}

func (t *slowTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	time.Sleep(t.delay)

	t.mu.Lock()
	t.calls++
	t.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":1}`)),
		Header:     make(http.Header),
	}, nil
}

func (t *slowTransport) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func waitForRequests(t *testing.T, transport *captureTransport, targetCount int) []capturedRequest {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		requests := transport.Requests()
		if len(requests) >= targetCount {
			return requests
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d analytics request(s)", targetCount)
	return nil
}

func TestPostHogTracker_Track_SanitizesAndAddsCommonProps(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Provider:            "posthog",
		PostHogAPIKey:       "ph-test-key",
		PostHogHost:         "https://us.i.posthog.com",
		PostHogDisableGeoIP: true,
		SourceSystem:        "api",
		Environment:         "staging",
		EventVersion:        "v9",
		HTTPClient:          client,
	})

	tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"email":     "private@openmentor.io",
		"name":      "Private Name",
		"mentor_id": "mentor-123",
		"outcome":   "success",
	})

	requests := waitForRequests(t, transport, 1)
	body := requests[0].Body
	url := requests[0].URL

	assert.Contains(t, url, "https://us.i.posthog.com/capture/")
	assert.Contains(t, body, `"api_key":"ph-test-key"`)
	assert.Contains(t, body, `"event":"mentee_contact_submitted"`)
	assert.Contains(t, body, `"distinct_id":"mentor:mentor-123"`)
	assert.Contains(t, body, `"source_system":"api"`)
	assert.Contains(t, body, `"environment":"staging"`)
	assert.Contains(t, body, `"event_version":"v9"`)
	assert.Contains(t, body, `"mentor_id":"mentor-123"`)
	assert.Contains(t, body, `"outcome":"success"`)
	assert.NotContains(t, body, "private@openmentor.io")
	assert.NotContains(t, body, "Private Name")
}

func TestPostHogTracker_Track_AddsTraceIDFromContext(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Provider:      "posthog",
		PostHogAPIKey: "ph-test-key",
		PostHogHost:   "https://us.i.posthog.com",
		SourceSystem:  "api",
		Environment:   "staging",
		EventVersion:  "v9",
		HTTPClient:    client,
	})

	traceID := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanID := trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	tracker.Track(ctx, EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"mentor_id": "mentor-123",
	})

	requests := waitForRequests(t, transport, 1)
	assert.Contains(t, requests[0].Body, `"trace_id":"0102030405060708090a0b0c0d0e0f10"`)
}

func TestPostHogTracker_Track_NoTraceIDWithoutActiveSpan(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Provider:      "posthog",
		PostHogAPIKey: "ph-test-key",
		PostHogHost:   "https://us.i.posthog.com",
		SourceSystem:  "api",
		Environment:   "staging",
		EventVersion:  "v9",
		HTTPClient:    client,
	})

	tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"mentor_id": "mentor-123",
	})

	requests := waitForRequests(t, transport, 1)
	assert.NotContains(t, requests[0].Body, `"trace_id"`)
}

func TestPostHogTracker_ImplicitProvider_UsesPostHogWhenEnabled(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		PostHogEnabled: true,
		PostHogAPIKey:  "ph-test-key",
		PostHogHost:    "https://us.i.posthog.com",
		SourceSystem:   "api",
		Environment:    "staging",
		EventVersion:   "v9",
		HTTPClient:     client,
	})

	tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"mentor_id": "mentor-123",
		"outcome":   "success",
	})

	requests := waitForRequests(t, transport, 1)
	assert.Contains(t, requests[0].URL, "https://us.i.posthog.com/capture/")
}

func TestTracker_Track_DoesNotBlockOnSlowNetwork(t *testing.T) {
	transport := &slowTransport{delay: 300 * time.Millisecond}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Provider:      "posthog",
		PostHogAPIKey: "ph-test-key",
		PostHogHost:   "https://us.i.posthog.com",
		SourceSystem:  "api",
		Environment:   "staging",
		EventVersion:  "v9",
		HTTPClient:    client,
	})

	startedAt := time.Now()
	tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"mentor_id": "mentor-123",
		"outcome":   "success",
	})
	elapsed := time.Since(startedAt)

	assert.Less(t, elapsed, 100*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if transport.Calls() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for async analytics worker request")
}

func TestNewTracker_DisabledReturnsNoop(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(&Config{
		PostHogEnabled: false,
		PostHogAPIKey:  "",
	})

	assert.IsType(t, NoopTracker{}, tracker)
}

func TestNewTracker_PostHogRequestedButUnconfiguredReturnsNoop(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(&Config{
		Provider: "posthog",
	})

	assert.IsType(t, NoopTracker{}, tracker)
}

func TestNewTracker_UnsupportedProviderReturnsNoop(t *testing.T) {
	t.Parallel()

	tracker := NewTracker(&Config{
		Provider:      "unsupported-provider",
		PostHogAPIKey: "ph-test-key",
		PostHogHost:   "https://us.i.posthog.com",
	})

	assert.IsType(t, NoopTracker{}, tracker)
}
