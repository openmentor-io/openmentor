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
	"github.com/stretchr/testify/require"
)

// gateTransport holds every request until it is released, so a test can put a
// known number of events in the queue with none of them yet sent — the state a
// SIGTERM used to discard silently.
type gateTransport struct {
	release chan struct{}

	mu    sync.Mutex
	calls int
}

func newGateTransport() *gateTransport {
	return &gateTransport{release: make(chan struct{})}
}

func (t *gateTransport) RoundTrip(*http.Request) (*http.Response, error) {
	<-t.release

	t.mu.Lock()
	t.calls++
	t.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":1}`)),
		Header:     make(http.Header),
	}, nil
}

func (t *gateTransport) Calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func newDrainTracker(client *http.Client, queueSize int) *AnalyticsTracker {
	return NewTracker(&Config{
		Provider:      "posthog",
		PostHogAPIKey: "ph-test-key",
		PostHogHost:   "https://eu.i.posthog.com",
		SourceSystem:  "api",
		Environment:   "test",
		QueueSize:     queueSize,
		HTTPClient:    client,
	}).(*AnalyticsTracker)
}

// TestCloseDrainsQueuedEvents is the whole point of C12's tracker item: events
// sitting in the queue when shutdown begins must be sent, not discarded.
func TestCloseDrainsQueuedEvents(t *testing.T) {
	transport := newGateTransport()
	tracker := newDrainTracker(&http.Client{Transport: transport}, 32)

	const events = 12
	for range events {
		tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-1"), nil)
	}

	// Nothing has been sent yet: the sender is blocked on the gate.
	assert.Equal(t, 0, transport.Calls())

	close(transport.release)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, tracker.Close(ctx))

	assert.Equal(t, events, transport.Calls(),
		"Close must not return until every queued event has been sent")
}

// The drain is bounded: a PostHog that never answers costs the deadline and not
// the deploy. Close reports the timeout rather than pretending it drained.
func TestCloseGivesUpAtTheDeadline(t *testing.T) {
	transport := newGateTransport() // never released
	tracker := newDrainTracker(&http.Client{Transport: transport}, 32)

	for range 4 {
		tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-1"), nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := tracker.Close(ctx)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 3*time.Second, "Close must not outlive its context")

	close(transport.release)
}

// Track after Close must be a no-op, not a panic. The queue is a closed channel
// by then, and a send on it would take the whole process down mid-shutdown —
// turning a dropped analytics event into a crash loop.
func TestTrackAfterCloseDoesNotPanic(t *testing.T) {
	transport := newGateTransport()
	close(transport.release)
	tracker := newDrainTracker(&http.Client{Transport: transport}, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, tracker.Close(ctx))

	assert.NotPanics(t, func() {
		tracker.Track(context.Background(), EventMenteeContactSubmitted, MentorDistinctID("mentor-1"), nil)
	})
}

// Close is idempotent: the two binaries each call it once, but a double call
// (a deferred close plus an explicit one) must not close the queue twice.
func TestCloseIsIdempotent(t *testing.T) {
	transport := newGateTransport()
	close(transport.release)
	tracker := newDrainTracker(&http.Client{Transport: transport}, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, tracker.Close(ctx))
	assert.NotPanics(t, func() { _ = tracker.Close(ctx) })
}

// The package-level Close helper is what the binaries call, and it must work for
// a tracker that buffers nothing — a provider-less deployment gets NoopTracker.
func TestPackageCloseHandlesNoopTracker(t *testing.T) {
	require.NoError(t, Close(context.Background(), NoopTracker{}))
}

// The default ingest host is the EU region, matching config.go's POSTHOG_HOST
// default. A US default here is a silent cross-region transfer for any caller
// that leaves the host unset (C12).
func TestDefaultPostHogHostIsEUAndIsWhatAHostlessConfigUses(t *testing.T) {
	assert.Equal(t, "https://eu.i.posthog.com", DefaultPostHogHost)
	assert.Equal(t, "https://eu.i.posthog.com/capture/", normalizePostHogEndpoint("", ""))
}
