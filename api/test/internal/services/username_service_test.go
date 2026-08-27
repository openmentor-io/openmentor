package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
)

// usernameMockRepo implements services.UsernameRepository
type usernameMockRepo struct {
	mentor *models.Mentor
	getErr error

	taken          bool
	takenErr       error
	takenSlug      string // records what was checked
	takenForID     string
	lastChange     *time.Time
	changeErr      error
	changedTo      string
	changedBy      string
	changeOld      string // oldSlug ChangeSlug returns
	changeCalls    int
	changeCooldown time.Duration // cooldown arg last passed to ChangeSlug
}

func (m *usernameMockRepo) IsSlugTaken(ctx context.Context, s, excludeMentorID string) (bool, error) {
	m.takenSlug = s
	m.takenForID = excludeMentorID
	return m.taken, m.takenErr
}

func (m *usernameMockRepo) LatestMentorSlugChange(ctx context.Context, mentorID string) (*time.Time, error) {
	return m.lastChange, nil
}

func (m *usernameMockRepo) ChangeSlug(ctx context.Context, mentorID, newSlug, changedBy string, cooldown time.Duration, now time.Time) (string, error) {
	m.changeCalls++
	m.changeCooldown = cooldown
	if m.changeErr != nil {
		return "", m.changeErr
	}
	m.changedTo = newSlug
	m.changedBy = changedBy
	return m.changeOld, nil
}

func (m *usernameMockRepo) GetByMentorId(ctx context.Context, mentorID string, opts models.FilterOptions) (*models.Mentor, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.mentor, nil
}

func newUsernameService(repo *usernameMockRepo, now time.Time) *services.UsernameService {
	svc := services.NewUsernameService(repo, nil)
	svc.SetNowFunc(func() time.Time { return now })
	return svc
}

func TestCheckAvailability_Available(t *testing.T) {
	repo := &usernameMockRepo{}
	svc := newUsernameService(repo, time.Now())

	res, err := svc.CheckAvailability(context.Background(), "  John-Doe ", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Available || res.Username != "john-doe" || res.Reason != "" {
		t.Fatalf("expected normalized available result, got %+v", res)
	}
	if repo.takenSlug != "john-doe" {
		t.Fatalf("expected taken-check for john-doe, got %q", repo.takenSlug)
	}
}

func TestCheckAvailability_Taken(t *testing.T) {
	repo := &usernameMockRepo{taken: true}
	svc := newUsernameService(repo, time.Now())

	res, err := svc.CheckAvailability(context.Background(), "john-doe", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Available || res.Reason != "taken" {
		t.Fatalf("expected taken, got %+v", res)
	}
}

func TestCheckAvailability_InvalidAndReserved(t *testing.T) {
	repo := &usernameMockRepo{}
	svc := newUsernameService(repo, time.Now())

	cases := []struct {
		input  string
		reason string
	}{
		{"ab", "invalid"},          // too short
		{"John Doe!", "invalid"},   // bad characters
		{"-leading", "invalid"},    // bad format
		{"admin", "reserved"},      // reserved word
		{"login", "reserved"},      // mentor-area route
		{"openmentor", "reserved"}, // brand
		// "mentor" and anything built from it, wherever the stem sits.
		{"mentor", "reserved"},
		{"anna-mentor", "reserved"},
		{"mentoring", "reserved"},
		{"m3nt0r", "reserved"},
	}
	for _, tc := range cases {
		res, err := svc.CheckAvailability(context.Background(), tc.input, "")
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", tc.input, err)
		}
		if res.Available || res.Reason != tc.reason {
			t.Fatalf("%q: expected reason %q, got %+v", tc.input, tc.reason, res)
		}
		if repo.changeCalls != 0 {
			t.Fatalf("%q: availability check must not change anything", tc.input)
		}
	}
}

func TestCheckAvailability_OwnCurrentSlugIsAvailable(t *testing.T) {
	repo := &usernameMockRepo{
		mentor: &models.Mentor{Slug: "john-doe"},
		taken:  true, // even if the taken-check would say taken
	}
	svc := newUsernameService(repo, time.Now())

	res, err := svc.CheckAvailability(context.Background(), "john-doe", "mentor-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Available {
		t.Fatalf("own current slug should read as available, got %+v", res)
	}
}

func TestCheckAvailability_PassesExcludeMentorID(t *testing.T) {
	repo := &usernameMockRepo{mentor: &models.Mentor{Slug: "current"}}
	svc := newUsernameService(repo, time.Now())

	if _, err := svc.CheckAvailability(context.Background(), "new-name", "mentor-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.takenForID != "mentor-1" {
		t.Fatalf("expected exclude mentor id to be forwarded, got %q", repo.takenForID)
	}
}

func TestStatus_CooldownActive(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	lastChange := now.Add(-5 * 24 * time.Hour) // 5 days ago, 14-day cooldown
	repo := &usernameMockRepo{
		mentor:     &models.Mentor{Slug: "john-doe"},
		lastChange: &lastChange,
	}
	svc := newUsernameService(repo, now)

	status, err := svc.Status(context.Background(), "mentor-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.CanChange {
		t.Fatal("expected cooldown to block change")
	}
	expectedNext := lastChange.Add(services.UsernameChangeCooldown)
	if status.NextChangeAt == nil || !status.NextChangeAt.Equal(expectedNext) {
		t.Fatalf("expected nextChangeAt %v, got %v", expectedNext, status.NextChangeAt)
	}
}

func TestStatus_CooldownExpired(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	lastChange := now.Add(-15 * 24 * time.Hour)
	repo := &usernameMockRepo{
		mentor:     &models.Mentor{Slug: "john-doe"},
		lastChange: &lastChange,
	}
	svc := newUsernameService(repo, now)

	status, err := svc.Status(context.Background(), "mentor-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.CanChange || status.NextChangeAt != nil {
		t.Fatalf("expected change allowed after cooldown, got %+v", status)
	}
}

func TestChange_MentorCooldownBlocks(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	lastChange := now.Add(-24 * time.Hour)
	repo := &usernameMockRepo{lastChange: &lastChange}
	svc := newUsernameService(repo, now)

	_, err := svc.Change(context.Background(), "mentor-1", "new-name", "mentor")
	var cooldown *services.CooldownError
	if !errors.As(err, &cooldown) {
		t.Fatalf("expected CooldownError, got %v", err)
	}
	expectedNext := lastChange.Add(services.UsernameChangeCooldown)
	if !cooldown.NextChangeAt.Equal(expectedNext) {
		t.Fatalf("expected nextChangeAt %v, got %v", expectedNext, cooldown.NextChangeAt)
	}
	if repo.changeCalls != 0 {
		t.Fatal("ChangeSlug must not be called during cooldown")
	}
}

func TestChange_NoOpDuringCooldownSucceeds(t *testing.T) {
	// A mentor in cooldown who re-submits their CURRENT username must get a
	// success (no-op), not a spurious 429 — availability reports it available.
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	lastChange := now.Add(-24 * time.Hour) // well inside the 14-day cooldown
	repo := &usernameMockRepo{
		mentor:     &models.Mentor{Slug: "john-doe"},
		lastChange: &lastChange,
	}
	svc := newUsernameService(repo, now)

	username, err := svc.Change(context.Background(), "mentor-1", "john-doe", "mentor")
	if err != nil {
		t.Fatalf("no-op change during cooldown should succeed, got %v", err)
	}
	if username != "john-doe" {
		t.Fatalf("expected current username returned, got %q", username)
	}
	if repo.changeCalls != 0 {
		t.Fatal("no-op must not reach ChangeSlug")
	}
}

func TestChange_AdminBypassesCooldown(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	lastChange := now.Add(-time.Hour) // very recent mentor change
	repo := &usernameMockRepo{lastChange: &lastChange, changeOld: "old-name"}
	svc := newUsernameService(repo, now)

	username, err := svc.Change(context.Background(), "mentor-1", "new-name", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if username != "new-name" || repo.changedBy != "admin" {
		t.Fatalf("expected admin change to new-name, got %q by %q", username, repo.changedBy)
	}
	if repo.changeCooldown != 0 {
		t.Fatalf("admin change must pass cooldown=0 (no enforcement), got %v", repo.changeCooldown)
	}
}

func TestChange_MentorPassesCooldownToRepo(t *testing.T) {
	repo := &usernameMockRepo{changeOld: "old-name"}
	svc := newUsernameService(repo, time.Now())

	if _, err := svc.Change(context.Background(), "mentor-1", "new-name", "mentor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.changeCooldown != services.UsernameChangeCooldown {
		t.Fatalf("mentor change must forward the 14-day cooldown to the repo, got %v", repo.changeCooldown)
	}
}

func TestChange_MapsRepoCooldownError(t *testing.T) {
	// The authoritative in-transaction cooldown lives in the repo; the service
	// must surface it as its own CooldownError (429 + nextChangeAt).
	next := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	repo := &usernameMockRepo{changeErr: &repository.CooldownError{NextChangeAt: next}}
	svc := newUsernameService(repo, time.Now())

	_, err := svc.Change(context.Background(), "mentor-1", "new-name", "mentor")
	var cooldown *services.CooldownError
	if !errors.As(err, &cooldown) {
		t.Fatalf("expected services.CooldownError, got %v", err)
	}
	if !cooldown.NextChangeAt.Equal(next) {
		t.Fatalf("expected nextChangeAt %v, got %v", next, cooldown.NextChangeAt)
	}
}

func TestChange_TakenMapsToErrUsernameTaken(t *testing.T) {
	repo := &usernameMockRepo{changeErr: repository.ErrSlugTaken}
	svc := newUsernameService(repo, time.Now())

	_, err := svc.Change(context.Background(), "mentor-1", "new-name", "mentor")
	if !errors.Is(err, services.ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestChange_ValidationBeforeRepo(t *testing.T) {
	repo := &usernameMockRepo{}
	svc := newUsernameService(repo, time.Now())

	if _, err := svc.Change(context.Background(), "mentor-1", "Bad Name!", "mentor"); !errors.Is(err, slug.ErrUsernameBadFormat) {
		t.Fatalf("expected format error, got %v", err)
	}
	if _, err := svc.Change(context.Background(), "mentor-1", "admin", "admin"); !errors.Is(err, slug.ErrUsernameReserved) {
		t.Fatalf("expected reserved error (also for admins), got %v", err)
	}
	if _, err := svc.Change(context.Background(), "mentor-1", "anna-mentor", "mentor"); !errors.Is(err, slug.ErrUsernameMentorDerivative) {
		t.Fatalf("expected mentor-derivative error, got %v", err)
	}
	if repo.changeCalls != 0 {
		t.Fatal("ChangeSlug must not be called for invalid usernames")
	}
}

func TestChange_NormalizesInput(t *testing.T) {
	repo := &usernameMockRepo{changeOld: "old"}
	svc := newUsernameService(repo, time.Now())

	username, err := svc.Change(context.Background(), "mentor-1", "  New-Name ", "mentor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if username != "new-name" || repo.changedTo != "new-name" {
		t.Fatalf("expected normalized new-name, got %q / repo %q", username, repo.changedTo)
	}
}
