package services_test

import (
	"context"
	"errors"
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
	"github.com/openmentor-io/openmentor/api/pkg/reviewtoken"
)

// reviewCapability is a live LEGACY review request_id: whoever holds it can read
// the mentor's name and submit a review as that mentee, so it must not reach
// analytics or the logs (P14).
const reviewCapability = "11111111-2222-4333-8444-555555555555"

// captchaSentinel is the Turnstile token the browser submits alongside it.
const captchaSentinel = "0.captcha-token-sentinel-value.abcdef"

// reviewTokenSentinel is a well-formed H4 capability with a recognizable body.
// Fabricated on purpose — no real minted token may appear in the repository, a
// test log or CI output.
const reviewTokenSentinel = reviewtoken.Prefix + "SENTINELreviewTokenValueAAAAAAAAAAAAAAAAAAA"

func init() { metrics.Init("openmentor-review-telemetry-test") }

// legacyEnabledConfig keeps the dual-read window open, which is what production
// runs until the H4 cutover.
func legacyEnabledConfig() *config.Config {
	return &config.Config{Review: config.ReviewConfig{LegacyRequestIDLinksEnabled: true}}
}

// fakeReviewRepo implements services.ReviewRepository.
type fakeReviewRepo struct {
	result    *repository.ReviewCheckResult
	checkErr  error
	submitErr error
	reviewID  string

	checkedIDs    []string
	submittedIDs  []string
	checkedHashes []string
	spentHashes   []string
}

func (f *fakeReviewRepo) CheckCanSubmitReview(_ context.Context, requestID string) (*repository.ReviewCheckResult, error) {
	f.checkedIDs = append(f.checkedIDs, requestID)
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return f.result, nil
}

func (f *fakeReviewRepo) SubmitReviewForRequest(_ context.Context, requestID string, _ repository.ReviewContent) (*repository.ReviewSubmission, error) {
	f.submittedIDs = append(f.submittedIDs, requestID)
	return f.submission()
}

func (f *fakeReviewRepo) CheckCanSubmitReviewByToken(_ context.Context, tokenHash string) (*repository.ReviewCheckResult, error) {
	f.checkedHashes = append(f.checkedHashes, tokenHash)
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if f.result != nil && f.result.MentorName == "" && !f.result.CanSubmit {
		// The token path has no "request not found" answer: a dead capability is
		// the only way not to resolve.
		return nil, repository.ErrReviewTokenInvalid
	}
	return f.result, nil
}

func (f *fakeReviewRepo) SubmitReviewWithToken(_ context.Context, tokenHash string, _ repository.ReviewContent) (*repository.ReviewSubmission, error) {
	f.spentHashes = append(f.spentHashes, tokenHash)
	return f.submission()
}

func (f *fakeReviewRepo) submission() (*repository.ReviewSubmission, error) {
	switch {
	case f.submitErr != nil:
		return nil, f.submitErr
	case f.checkErr != nil:
		return nil, f.checkErr
	case f.result == nil || !f.result.CanSubmit:
		return nil, repository.ErrReviewIneligible
	}
	return &repository.ReviewSubmission{ReviewID: f.reviewID, MentorID: f.result.MentorID}, nil
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

// TestReviewServiceTelemetryOmitsRequestCapability drives the LEGACY review entry
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
			service := services.NewReviewService(tc.repo, legacyEnabledConfig(), captchaPassingClient{}, tracker)

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

// TestReviewTokenNeverReachesTelemetry is the H4 sentinel at the service layer:
// the raw capability may appear in exactly one place — the value handed to
// reviewtoken.Hash — and never in an event property, a distinct id or a log line.
//
// It also pins that the repository is given the HASH and never the raw token.
func TestReviewTokenNeverReachesTelemetry(t *testing.T) {
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
			name: "spent_or_expired",
			repo: &fakeReviewRepo{
				result:    &repository.ReviewCheckResult{CanSubmit: false},
				submitErr: repository.ErrReviewTokenInvalid,
			},
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
			service := services.NewReviewService(tc.repo, legacyEnabledConfig(), captchaPassingClient{}, tracker)

			//nolint:errcheck // the error paths are the point; telemetry is what is asserted
			service.CheckReviewByToken(context.Background(), reviewTokenSentinel)
			//nolint:errcheck // same
			service.SubmitReviewWithToken(context.Background(), reviewTokenSentinel, &models.SubmitReviewRequest{
				Token:        reviewTokenSentinel,
				MentorReview: "great session",
				CaptchaToken: captchaSentinel,
			})

			if len(tracker.calls) == 0 {
				t.Fatal("no analytics events recorded: the test drove nothing")
			}
			for _, call := range tracker.calls {
				assertNoReviewSecrets(t, "distinct_id of "+call.event, call.distinctID)
				assertNoReviewSecrets(t, "properties of "+call.event, fmt.Sprint(call.properties))
			}
			for _, entry := range logs.All() {
				assertNoReviewSecrets(t, "log entry", entry.Message+" "+fmt.Sprint(entry.ContextMap()))
			}

			wantHash := reviewtoken.Hash(reviewTokenSentinel)
			for _, got := range append(tc.repo.checkedHashes, tc.repo.spentHashes...) {
				if got != wantHash {
					t.Errorf("repository received %q, want the sha256 of the token", got)
				}
			}
			if len(tc.repo.checkedHashes) == 0 {
				t.Error("the token check never reached the repository")
			}
		})
	}
}

// TestReviewTokenPathRevealsNothingForDeadCapability covers the acceptance
// criterion "expired or consumed tokens reveal no request details": the answer
// carries no mentor name, and a malformed and a spent token are indistinguishable.
func TestReviewTokenPathRevealsNothingForDeadCapability(t *testing.T) {
	observeServiceLogs(t)
	cases := map[string]string{
		"malformed":   "not-a-token",
		"well-formed": reviewTokenSentinel,
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			// The repo would happily disclose a mentor; the service must not ask.
			repo := &fakeReviewRepo{
				result:    &repository.ReviewCheckResult{CanSubmit: false, MentorID: "mentor-1", MentorName: "John Doe"},
				submitErr: repository.ErrReviewTokenInvalid,
				checkErr:  repository.ErrReviewTokenInvalid,
			}
			service := services.NewReviewService(repo, legacyEnabledConfig(), captchaPassingClient{}, &reviewTracker{})

			resp, err := service.CheckReviewByToken(context.Background(), token)
			if !errors.Is(err, services.ErrReviewLinkInvalid) {
				t.Fatalf("CheckReviewByToken error = %v, want ErrReviewLinkInvalid", err)
			}
			if resp.MentorName != "" {
				t.Errorf("a dead capability disclosed the mentor name %q", resp.MentorName)
			}
			if resp.CanSubmit {
				t.Error("a dead capability reported canSubmit=true")
			}

			submitResp, submitErr := service.SubmitReviewWithToken(context.Background(), token, &models.SubmitReviewRequest{
				Token:        token,
				MentorReview: "great session",
				CaptchaToken: captchaSentinel,
			})
			if !errors.Is(submitErr, services.ErrReviewLinkInvalid) {
				t.Fatalf("SubmitReviewWithToken error = %v, want ErrReviewLinkInvalid", submitErr)
			}
			if submitResp.Success {
				t.Error("a dead capability produced a successful submission")
			}
		})
	}
}

// TestLegacyReviewPathRefusedWhenSwitchedOff is the cutover: with
// REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED off, the request-id endpoints refuse
// before touching the database.
func TestLegacyReviewPathRefusedWhenSwitchedOff(t *testing.T) {
	observeServiceLogs(t)
	repo := &fakeReviewRepo{
		result:   &repository.ReviewCheckResult{CanSubmit: true, MentorID: "mentor-1", MentorName: "John Doe"},
		reviewID: "review-1",
	}
	// The zero value is "off"; config.Load defaults it to on for production.
	service := services.NewReviewService(repo, &config.Config{}, captchaPassingClient{}, &reviewTracker{})

	if _, err := service.CheckReview(context.Background(), reviewCapability); !errors.Is(err, services.ErrReviewLegacyPathDisabled) {
		t.Errorf("CheckReview error = %v, want ErrReviewLegacyPathDisabled", err)
	}
	_, err := service.SubmitReview(context.Background(), reviewCapability, &models.SubmitReviewRequest{
		MentorReview: "great session",
		CaptchaToken: captchaSentinel,
	})
	if !errors.Is(err, services.ErrReviewLegacyPathDisabled) {
		t.Errorf("SubmitReview error = %v, want ErrReviewLegacyPathDisabled", err)
	}
	if len(repo.checkedIDs) != 0 || len(repo.submittedIDs) != 0 {
		t.Errorf("the disabled legacy path still hit the database: %v %v", repo.checkedIDs, repo.submittedIDs)
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
	service := services.NewReviewService(repo, legacyEnabledConfig(), captchaPassingClient{}, tracker)

	if _, err := service.SubmitReviewWithToken(context.Background(), reviewTokenSentinel, &models.SubmitReviewRequest{
		Token:        reviewTokenSentinel,
		MentorReview: "great session",
		CaptchaToken: captchaSentinel,
	}); err != nil {
		t.Fatalf("SubmitReviewWithToken failed: %v", err)
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
	if last.properties["link_type"] != "token" {
		t.Errorf("link_type = %v, want token (the cutover signal)", last.properties["link_type"])
	}
}

func assertNoReviewSecrets(t *testing.T, where, value string) {
	t.Helper()
	for name, secret := range map[string]string{
		"review capability": reviewCapability,
		"captcha token":     captchaSentinel,
		"review token":      reviewTokenSentinel,
	} {
		if strings.Contains(value, secret) {
			t.Errorf("%s leaked the %s: %s", where, name, value)
		}
	}
}
