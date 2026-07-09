package analytics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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

func TestMixpanelTracker_Track_SanitizesAndAddsCommonProps(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Enabled:      true,
		Token:        "test-token",
		Endpoint:     "https://mixpanel.invalid/track",
		SourceSystem: "api",
		Environment:  "staging",
		EventVersion: "v9",
		HTTPClient:   client,
	})

	tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"email":     "private@openmentor.io",
		"name":      "Private Name",
		"mentor_id": "mentor-123",
		"outcome":   "success",
	})

	requests := waitForRequests(t, transport, 1)
	body := requests[0].Body

	assert.Contains(t, body, EventMenteeContactSubmitted)
	assert.Contains(t, body, `"token":"test-token"`)
	assert.Contains(t, body, `"distinct_id":"mentor:mentor-123"`)
	assert.Contains(t, body, `"source_system":"api"`)
	assert.Contains(t, body, `"environment":"staging"`)
	assert.Contains(t, body, `"event_version":"v9"`)
	assert.Contains(t, body, `"mentor_id":"mentor-123"`)
	assert.Contains(t, body, `"outcome":"success"`)
	assert.NotContains(t, body, "private@openmentor.io")
	assert.NotContains(t, body, "Private Name")
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

func TestDualTracker_Track_SendsToBothProviders(t *testing.T) {
	t.Parallel()

	transport := &captureTransport{}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Provider:         "dual",
		MixpanelToken:    "mx-token",
		MixpanelEndpoint: "https://mixpanel.invalid/track",
		PostHogAPIKey:    "ph-test-key",
		PostHogHost:      "https://us.i.posthog.com",
		SourceSystem:     "api",
		Environment:      "staging",
		EventVersion:     "v9",
		HTTPClient:       client,
	})

	tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-123"), map[string]interface{}{
		"mentor_id": "mentor-123",
		"outcome":   "success",
	})

	requests := waitForRequests(t, transport, 2)

	var hasMixpanel bool
	var hasPostHog bool
	for _, request := range requests {
		if strings.Contains(request.URL, "mixpanel.invalid/track") {
			hasMixpanel = true
		}
		if strings.Contains(request.URL, "posthog.com/capture/") {
			hasPostHog = true
		}
	}

	assert.True(t, hasMixpanel, "expected mixpanel request")
	assert.True(t, hasPostHog, "expected posthog request")
}

func TestTracker_Track_DoesNotBlockOnSlowNetwork(t *testing.T) {
	transport := &slowTransport{delay: 300 * time.Millisecond}
	client := &http.Client{Transport: transport}
	tracker := NewTracker(&Config{
		Enabled:      true,
		Token:        "test-token",
		Endpoint:     "https://mixpanel.invalid/track",
		SourceSystem: "api",
		Environment:  "staging",
		EventVersion: "v9",
		HTTPClient:   client,
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
		Enabled: false,
		Token:   "",
	})

	assert.IsType(t, NoopTracker{}, tracker)
}
