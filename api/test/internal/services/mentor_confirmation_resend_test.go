package services_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// rotatingConfirmationRepo is the confirmation flow's row: a token is looked up
// by value, and RotateConfirmationToken REPLACES it — which is exactly what a
// resend does, so the token a mentor holds after one resend is not the token
// they submitted.
type rotatingConfirmationRepo struct {
	mu        sync.Mutex
	token     string
	mentorID  string
	status    string
	expiresAt time.Time
	rotations int
}

func (r *rotatingConfirmationRepo) GetByConfirmationToken(_ context.Context, token string) (*models.MentorConfirmation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token == "" || token != r.token {
		return nil, nil
	}
	return &models.MentorConfirmation{
		MentorID:  r.mentorID,
		Name:      "John Doe",
		Email:     "john@example.com",
		Status:    r.status,
		ExpiresAt: r.expiresAt,
	}, nil
}

func (r *rotatingConfirmationRepo) ConsumeConfirmationToken(_ context.Context, token string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token == "" || token != r.token || r.status != "draft" || time.Now().After(r.expiresAt) {
		return false, nil
	}
	r.status = "pending"
	r.token = ""
	return true, nil
}

func (r *rotatingConfirmationRepo) RotateConfirmationToken(
	_ context.Context, oldToken, newToken string, expiresAt time.Time,
) (bool, error) {

	r.mu.Lock()
	defer r.mu.Unlock()
	if oldToken == "" || oldToken != r.token || r.status != "draft" {
		return false, nil
	}
	r.token = newToken
	r.expiresAt = expiresAt
	r.rotations++
	return true, nil
}

func (r *rotatingConfirmationRepo) currentToken() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.token
}

func draftConfirmationRepo() *rotatingConfirmationRepo {
	return &rotatingConfirmationRepo{
		token:     "mcf_first",
		mentorID:  "mentor-1",
		status:    "draft",
		expiresAt: time.Now().Add(time.Hour),
	}
}

// newResendService wires the confirmation service with a real limiter of the
// given burst and no meaningful refill during a test.
func newResendService(repo services.MentorConfirmationRepository, burst int) *services.MentorConfirmationService {
	// Empty config => no worker trigger URL => trigger.CallAsync is a no-op, so
	// no HTTP client is needed.
	return services.NewMentorConfirmationService(repo, &config.Config{}, nil, nil,
		middleware.NewRateLimiter(0.0001, burst))
}

// TestResendConfirmationLimitSurvivesTokenRotation is the whole point of keying
// the limit on the mentor id: every successful resend issues a FRESH token and
// emails it, so a limiter keyed on the submitted token hands the next attempt a
// brand-new bucket — unlimited resends from one mentor, each of them an email.
func TestResendConfirmationLimitSurvivesTokenRotation(t *testing.T) {
	repo := draftConfirmationRepo()
	svc := newResendService(repo, 1)

	if _, err := svc.ResendConfirmation(context.Background(), "mcf_first"); err != nil {
		t.Fatalf("first ResendConfirmation() error = %v, want nil", err)
	}

	rotated := repo.currentToken()
	if rotated == "" || rotated == "mcf_first" {
		t.Fatalf("token = %q, want a rotated token (the premise of this test)", rotated)
	}

	// The link the mentor just received carries `rotated`, so this is the exact
	// request the old token-keyed limiter could not see as a repeat.
	if _, err := svc.ResendConfirmation(context.Background(), rotated); !errors.Is(err, services.ErrConfirmationResendLimited) {
		t.Fatalf("second ResendConfirmation() error = %v, want ErrConfirmationResendLimited", err)
	}
	if repo.rotations != 1 {
		t.Errorf("token rotations = %d, want 1: a limited resend must not issue a token either", repo.rotations)
	}
}

// TestResendConfirmationLimitIsPerMentor: the budget is keyed, not global, so one
// mentor exhausting theirs must not lock every other mentor out — the failure
// mode the IP-keyed limiter had behind the shared BFF address.
func TestResendConfirmationLimitIsPerMentor(t *testing.T) {
	limiter := middleware.NewRateLimiter(0.0001, 1)

	first := draftConfirmationRepo()
	second := draftConfirmationRepo()
	second.mentorID = "mentor-2"

	firstSvc := services.NewMentorConfirmationService(first, &config.Config{}, nil, nil, limiter)
	secondSvc := services.NewMentorConfirmationService(second, &config.Config{}, nil, nil, limiter)

	if _, err := firstSvc.ResendConfirmation(context.Background(), "mcf_first"); err != nil {
		t.Fatalf("mentor-1 first resend error = %v, want nil", err)
	}
	if _, err := firstSvc.ResendConfirmation(context.Background(), first.currentToken()); !errors.Is(err, services.ErrConfirmationResendLimited) {
		t.Fatalf("mentor-1 second resend error = %v, want ErrConfirmationResendLimited", err)
	}
	if _, err := secondSvc.ResendConfirmation(context.Background(), "mcf_first"); err != nil {
		t.Fatalf("mentor-2 resend error = %v, want nil: mentor-1's budget is not theirs", err)
	}
}

// TestResendConfirmationBudgetIsNotSpentBeforeAResendHappens: the limit is
// consumed only once a resend is actually going to be attempted. A dead token or
// a mentor already past draft costs nobody budget, so neither a flood of
// guesses nor a stale open tab can lock a mentor out of their own resend.
func TestResendConfirmationBudgetIsNotSpentBeforeAResendHappens(t *testing.T) {
	repo := draftConfirmationRepo()
	svc := newResendService(repo, 1)

	for i := range 5 {
		if _, err := svc.ResendConfirmation(context.Background(), "mcf_guess"); !errors.Is(err, services.ErrConfirmationTokenInvalid) {
			t.Fatalf("guess %d: error = %v, want ErrConfirmationTokenInvalid", i, err)
		}
	}

	confirmed := draftConfirmationRepo()
	confirmed.status = "pending"
	confirmedSvc := newResendService(confirmed, 1)
	already, err := confirmedSvc.ResendConfirmation(context.Background(), "mcf_first")
	if err != nil || !already {
		t.Fatalf("resend for a confirmed mentor = (%v, %v), want (true, nil)", already, err)
	}
	if confirmed.rotations != 0 {
		t.Errorf("token rotations = %d, want 0 for a mentor past draft", confirmed.rotations)
	}

	// The one token in the bucket is still there for the real resend.
	if _, err := svc.ResendConfirmation(context.Background(), "mcf_first"); err != nil {
		t.Fatalf("ResendConfirmation() after the guesses error = %v, want nil", err)
	}
}
