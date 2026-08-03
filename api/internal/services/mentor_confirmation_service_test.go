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
)

// The services log through the package-level logger, which only the binaries
// initialize.
func TestMain(m *testing.M) {
	logger.Log = zap.NewNop()
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

func (f *fakeConfirmationRepo) ConfirmMentorEmail(_ context.Context, mentorID string) error {
	f.confirmed = append(f.confirmed, mentorID)
	return nil
}

func (f *fakeConfirmationRepo) SetEmailConfirmation(_ context.Context, mentorID, token string, expiresAt time.Time) error {
	f.rotations[mentorID] = expiresAt
	f.byToken[token] = &models.MentorConfirmation{
		MentorID: mentorID, Status: "draft", ExpiresAt: expiresAt,
	}
	return nil
}

func newConfirmationService(repo MentorConfirmationRepository) *MentorConfirmationService {
	// Empty trigger config: CallAsync skips, so no worker call escapes the test.
	return NewMentorConfirmationService(repo, &config.Config{}, nil, nil)
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
