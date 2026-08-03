package services_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// reviewCapability is a live review request_id: whoever holds it can read the
// mentor's name and submit a review as that mentee, so it must not reach
// analytics or the logs (P14).
const reviewCapability = "11111111-2222-4333-8444-555555555555"

// captchaSentinel is the Turnstile token the browser submits alongside it.
const captchaSentinel = "0.captcha-token-sentinel-value.abcdef"

func init() { metrics.Init("openmentor-review-telemetry-test") }

// fakeReviewRepo implements services.ReviewRepository.
type fakeReviewRepo struct {
	result   *repository.ReviewCheckResult
	checkErr error
	reviewID string

	checkedIDs []string
	createdIDs []string
}

func (f *fakeReviewRepo) CheckCanSubmitReview(_ context.Context, requestID string) (*repository.ReviewCheckResult, error) {
	f.checkedIDs = append(f.checkedIDs, requestID)
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return f.result, nil
}

func (f *fakeReviewRepo) CreateReview(_ context.Context, requestID, _, _, _ string) (string, error) {
	f.createdIDs = append(f.createdIDs, requestID)
	return f.reviewID, nil
}

var _ services.ReviewRepository = (*fakeReviewRepo)(nil)

// captchaPassingClient answers Turnstile's siteverify with a success.
type captchaPassingClient struct{}

func (captchaPassingClient) Post(_, _ string, _ io.Reader) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
		Header:     make(http.Header),
	}, nil
}

func (captchaPassingClient) Get(string) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected GET")
}

func (captchaPassingClient) Do(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do")
}

// trackedCall is one analytics.Tracker invocation.
type trackedCall struct {
	event      string
	distinctID string
	properties map[string]interface{}
}

type reviewTracker struct{ calls []trackedCall }

func (r *reviewTracker) Track(_ context.Context, event, distinctID string, properties map[string]interface{}) {
	r.calls = append(r.calls, trackedCall{event: event, distinctID: distinctID, properties: properties})
}

func observeServiceLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Log
	logger.Log = zap.New(core)
	t.Cleanup(func() { logger.Log = previous })

	return logs
}

// TestReviewServiceTelemetryOmitsRequestCapability drives both review entry
// points with a sentinel request id and captcha token and asserts neither shows
// up in an event property, a distinct_id or a log line.
func TestReviewServiceTelemetryOmitsRequestCapability(t *testing.T) {
	cases := []struct {
		name string
		repo *fakeReviewRepo
	}{
		{
			name: "eligible",
			repo: &fakeReviewRepo{
				result:   &repository.ReviewCheckResult{CanSubmit: true, MentorID: "mentor-1", MentorName: "John Doe"},
				reviewID: "review-1",
			},
		},
		{
			name: "not_found",
			repo: &fakeReviewRepo{result: &repository.ReviewCheckResult{CanSubmit: false}},
		},
		{
			name: "db_error",
			repo: &fakeReviewRepo{checkErr: fmt.Errorf("connection refused")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := observeServiceLogs(t)
			tracker := &reviewTracker{}
			service := services.NewReviewService(tc.repo, &config.Config{}, captchaPassingClient{}, tracker)

			//nolint:errcheck // the error paths are the point; telemetry is what is asserted
			service.CheckReview(context.Background(), reviewCapability)
			//nolint:errcheck // same
			service.SubmitReview(context.Background(), reviewCapability, &models.SubmitReviewRequest{
				MentorReview: "great session",
				CaptchaToken: captchaSentinel,
			})

			if len(tracker.calls) == 0 {
				t.Fatal("no analytics events recorded: the test drove nothing")
			}
			for _, call := range tracker.calls {
				assertNoReviewSecrets(t, "distinct_id of "+call.event, call.distinctID)
				assertNoReviewSecrets(t, "properties of "+call.event, fmt.Sprint(call.properties))
				if strings.HasPrefix(call.distinctID, "request:") {
					t.Errorf("%s still creates a request-keyed person: %q", call.event, call.distinctID)
				}
			}

			for _, entry := range logs.All() {
				assertNoReviewSecrets(t, "log entry", entry.Message+" "+fmt.Sprint(entry.ContextMap()))
			}

			// The repository still has to receive the real id, or the flow is broken.
			if len(tc.repo.checkedIDs) == 0 || tc.repo.checkedIDs[0] != reviewCapability {
				t.Errorf("repository received %v, want the real request id", tc.repo.checkedIDs)
			}
		})
	}
}

// TestReviewServiceAttributesSuccessToMentor pins the replacement identity: the
// events are still attributable, just to the mentor instead of the capability.
func TestReviewServiceAttributesSuccessToMentor(t *testing.T) {
	observeServiceLogs(t)
	repo := &fakeReviewRepo{
		result:   &repository.ReviewCheckResult{CanSubmit: true, MentorID: "mentor-1", MentorName: "John Doe"},
		reviewID: "review-1",
	}
	tracker := &reviewTracker{}
	service := services.NewReviewService(repo, &config.Config{}, captchaPassingClient{}, tracker)

	if _, err := service.SubmitReview(context.Background(), reviewCapability, &models.SubmitReviewRequest{
		MentorReview: "great session",
		CaptchaToken: captchaSentinel,
	}); err != nil {
		t.Fatalf("SubmitReview failed: %v", err)
	}

	last := tracker.calls[len(tracker.calls)-1]
	if last.distinctID != "mentor:mentor-1" {
		t.Errorf("distinct_id = %q, want mentor:mentor-1", last.distinctID)
	}
	if last.properties["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", last.properties["outcome"])
	}
	if last.properties["review_id"] != "review-1" {
		t.Errorf("review_id = %v, want review-1", last.properties["review_id"])
	}
}

func assertNoReviewSecrets(t *testing.T, where, value string) {
	t.Helper()
	for name, secret := range map[string]string{
		"review capability": reviewCapability,
		"captcha token":     captchaSentinel,
	} {
		if strings.Contains(value, secret) {
			t.Errorf("%s leaked the %s: %s", where, name, value)
		}
	}
}
