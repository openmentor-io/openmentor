package errortracking

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	posthog "github.com/posthog/posthog-go"

	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// reviewCapability stands in for a live client_requests id: whoever holds it can
// read the mentor's name and submit a review, so it must not reach PostHog (P14).
const reviewCapability = "11111111-2222-4333-8444-555555555555"

// recordingTransport captures the bodies the SDK actually puts on the wire, which
// is the only place that proves what left the process.
type recordingTransport struct{ bodies []string }

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		body, _ := io.ReadAll(req.Body) //nolint:errcheck
		t.bodies = append(t.bodies, string(body))
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":1}`)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// withRecordingClient points the package singleton at a client whose transport is
// captured, so a test can assert on the exported payload.
func withRecordingClient(t *testing.T) *recordingTransport {
	t.Helper()

	transport := &recordingTransport{}
	ph, err := posthog.NewWithConfig("phc_test", posthog.Config{
		Endpoint:  "http://posthog.invalid",
		Transport: transport,
		BatchSize: 1,
		Interval:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("posthog.NewWithConfig: %v", err)
	}

	previous := client
	client = &errorTrackingClient{ph: ph}
	t.Cleanup(func() { client = previous })

	return transport
}

// TestCaptureExceptionRedactsTheMessage covers the sink no middleware can reach:
// respondError calls CaptureException from inside the handler, before the
// observability middleware sanitizes c.Errors, and the payload goes to a
// third-party store.
func TestCaptureExceptionRedactsTheMessage(t *testing.T) {
	transport := withRecordingClient(t)

	// Verbatim shape from MentorRequestsHandler.handleRequestError.
	err := fmt.Errorf("failed to fetch request id=%q: request not found", reviewCapability)
	CaptureException(err, map[string]interface{}{"http_status": 500})
	Close()

	if len(transport.bodies) == 0 {
		t.Fatal("nothing was sent: the SDK did not flush, so the test proves nothing")
	}
	for _, body := range transport.bodies {
		if strings.Contains(body, reviewCapability) {
			t.Errorf("exception payload leaked the review capability: %s", body)
		}
		// Hashed rather than dropped, so the issue still ties to the request_ref
		// logged next to it.
		if !strings.Contains(body, redact.ID(reviewCapability)) {
			t.Errorf("exception payload carries no hashed reference: %s", body)
		}
		if !strings.Contains(body, "failed to fetch request") {
			t.Errorf("exception payload lost the reason, redaction is too broad: %s", body)
		}
	}
}

// TestCapturePanicRedactsThePanicValue covers panic(err), where err can be a
// wrapped repository error naming the row it failed on.
func TestCapturePanicRedactsThePanicValue(t *testing.T) {
	transport := withRecordingClient(t)

	CapturePanic(fmt.Errorf("failed to fetch client request %s: timeout", reviewCapability), []byte("goroutine 1 [running]:\n"))
	Close()

	if len(transport.bodies) == 0 {
		t.Fatal("nothing was sent: the SDK did not flush, so the test proves nothing")
	}
	for _, body := range transport.bodies {
		if strings.Contains(body, reviewCapability) {
			t.Errorf("panic payload leaked the review capability: %s", body)
		}
	}
}
