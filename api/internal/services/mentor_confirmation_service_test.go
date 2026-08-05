package services

// These tests pin the two behaviors the token-invalidation runbook
// (docs/runbooks/telemetry-leak-token-invalidation.md, step 3) depends on when it
// rotates live confirmation tokens:
//
//  1. ConfirmEmail refuses a token whose expiry has passed, so rotating a token
//     while keeping a stale expiry produces a link that is dead on arrival.
//  2. ResendConfirmation accepts an ALREADY EXPIRED token, which is the mentor's
//     only self-service way back — and the reason the runbook leaves expired
//     tokens alone instead of rotating them.

import (
	"context"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// The services log through the package-level logger and record Prometheus
// counters, both of which only the binaries initialize — an uninitialized counter
// vec is a nil pointer, so a metrics-recording path panics in tests without this.
func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
	metrics.Init("openmentor-services-test")
	os.Exit(m.Run())
}

// fakeConfirmationRepo is an in-memory MentorConfirmationRepository keyed by
// token, mirroring GetByConfirmationToken's lookup (which does NOT filter on
// expiry — the caller decides between confirm and resend).
type fakeConfirmationRepo struct {
	byToken   map[string]*models.MentorConfirmation
	confirmed []string
	rotations map[string]time.Time // mentor id -> new expiry
}

func newFakeConfirmationRepo() *fakeConfirmationRepo {
	return &fakeConfirmationRepo{
		byToken:   map[string]*models.MentorConfirmation{},
		rotations: map[string]time.Time{},
	}
}

func (f *fakeConfirmationRepo) GetByConfirmationToken(_ context.Context, token string) (*models.MentorConfirmation, error) {
	if mc, found := f.byToken[token]; found {
		copied := *mc
		return &copied, nil
	}
	return nil, nil
}

// ConsumeConfirmationToken models the atomic consumption: it applies only to a
// draft row whose window is still open, exactly like the SQL WHERE.
func (f *fakeConfirmationRepo) ConsumeConfirmationToken(_ context.Context, token string) (bool, error) {
	mc, found := f.byToken[token]
	if !found || mc.Status != "draft" || time.Now().After(mc.ExpiresAt) {
		return false, nil
	}
	delete(f.byToken, token)
	mc.Status = "pending"
	f.confirmed = append(f.confirmed, mc.MentorID)
	return true, nil
}

// RotateConfirmationToken models the compare-and-swap: it applies only while the
// OLD token is still the one on the row.
func (f *fakeConfirmationRepo) RotateConfirmationToken(
	_ context.Context, oldToken, newToken string, expiresAt time.Time,
) (bool, error) {

	mc, found := f.byToken[oldToken]
	if !found || mc.Status != "draft" {
		return false, nil
	}
	delete(f.byToken, oldToken)
	f.rotations[mc.MentorID] = expiresAt
	f.byToken[newToken] = &models.MentorConfirmation{
		MentorID: mc.MentorID, Status: "draft", ExpiresAt: expiresAt,
	}
	return true, nil
}

func newConfirmationService(repo MentorConfirmationRepository) *MentorConfirmationService {
	// Empty trigger config: CallAsync skips, so no worker call escapes the test.
	// nil resendLimiter: the constructor substitutes unlimitedResends, so the
	// per-mentor resend budget (D48) cannot make these redaction cases flaky —
	// test/internal/services/mentor_confirmation_resend_test.go covers the budget.
	return NewMentorConfirmationService(repo, &config.Config{}, nil, nil, nil)
}

func TestConfirmEmailRejectsAnExpiredToken(t *testing.T) {
	repo := newFakeConfirmationRepo()
	repo.byToken["mcf_expired"] = &models.MentorConfirmation{
		MentorID: "m1", Status: "draft", ExpiresAt: time.Now().Add(-time.Hour),
	}

	_, err := newConfirmationService(repo).ConfirmEmail(context.Background(), "mcf_expired")
	if err == nil || !isExpired(err) {
		t.Fatalf("ConfirmEmail(expired token) error = %v, want %v", err, ErrConfirmationTokenExpired)
	}
	if len(repo.confirmed) != 0 {
		t.Errorf("expired token still confirmed the mentor: %v", repo.confirmed)
	}
}

// TestResendConfirmationAcceptsAnExpiredToken is what makes an expired
// confirmation link recoverable: the mentor's dead link still reaches the resend
// endpoint, which issues a fresh token AND a fresh expiry.
func TestResendConfirmationAcceptsAnExpiredToken(t *testing.T) {
	repo := newFakeConfirmationRepo()
	repo.byToken["mcf_expired"] = &models.MentorConfirmation{
		MentorID: "m1", Status: "draft", ExpiresAt: time.Now().Add(-72 * time.Hour),
	}

	already, err := newConfirmationService(repo).ResendConfirmation(context.Background(), "mcf_expired")
	if err != nil {
		t.Fatalf("ResendConfirmation(expired token) = %v, want nil", err)
	}
	if already {
		t.Error("already = true for a draft mentor")
	}

	expiry, rotated := repo.rotations["m1"]
	if !rotated {
		t.Fatal("no fresh token stored: the mentor has no way back")
	}
	if !expiry.After(time.Now()) {
		t.Errorf("fresh token expires at %v, which is not in the future", expiry)
	}
}

func isExpired(err error) bool {
	return err == ErrConfirmationTokenExpired
}
