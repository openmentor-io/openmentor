package services

// Username service (D29): availability checks and the dedicated username
// (slug) change flow. Publicly the field is called "username"; internally it
// is mentors.slug. Changing it is a BREAKING action (shared links, OG cards),
// so it lives here — deliberately separate from the regular profile-save
// endpoints — with a 14-day cooldown for mentor-initiated changes. Admin
// changes skip the cooldown but go through the same history/redirect
// machinery so old links keep working.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
)

// UsernameChangeCooldown is the minimum interval between mentor-initiated
// username changes.
const UsernameChangeCooldown = 14 * 24 * time.Hour

// ErrUsernameTaken is returned when the requested username is unavailable.
var ErrUsernameTaken = errors.New("username already taken")

// CooldownError carries when the mentor may change their username again.
type CooldownError struct {
	NextChangeAt time.Time
}

func (e *CooldownError) Error() string {
	return fmt.Sprintf("username can be changed again at %s", e.NextChangeAt.Format(time.RFC3339))
}

// AvailabilityResult is the outcome of a username availability check.
type AvailabilityResult struct {
	// Username is the normalized form that was checked.
	Username string `json:"username"`
	// Available is true when the username can be claimed.
	Available bool `json:"available"`
	// Reason is one of "" (available), "taken", "invalid", "reserved".
	Reason string `json:"reason,omitempty"`
	// Message is a human-readable explanation for unavailable results.
	Message string `json:"message,omitempty"`
}

// UsernameStatus describes the current username and change eligibility for
// the dashboard card.
type UsernameStatus struct {
	Username     string     `json:"username"`
	CanChange    bool       `json:"canChange"`
	NextChangeAt *time.Time `json:"nextChangeAt,omitempty"`
}

// UsernameRepository is the narrow data-access surface this service needs.
type UsernameRepository interface {
	IsSlugTaken(ctx context.Context, slug, excludeMentorID string) (bool, error)
	LatestMentorSlugChange(ctx context.Context, mentorID string) (*time.Time, error)
	ChangeSlug(ctx context.Context, mentorID, newSlug, changedBy string, cooldown time.Duration, now time.Time) (string, error)
	GetByMentorId(ctx context.Context, mentorId string, opts models.FilterOptions) (*models.Mentor, error)
}

// UsernameService implements availability checks and username changes.
type UsernameService struct {
	repo    UsernameRepository
	tracker analytics.Tracker
	now     func() time.Time // injectable for tests
}

// NewUsernameService wires the username service.
func NewUsernameService(repo UsernameRepository, tracker analytics.Tracker) *UsernameService {
	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}
	return &UsernameService{repo: repo, tracker: tracker, now: time.Now}
}

// SetNowFunc overrides the service clock; for tests only.
func (s *UsernameService) SetNowFunc(now func() time.Time) {
	s.now = now
}

// CheckAvailability normalizes and validates the candidate username and
// checks whether it can be claimed. forMentorID (may be empty) excludes that
// mentor's own current slug and history rows, so "change back to my old
// username" and "keep my current one" both read as available.
func (s *UsernameService) CheckAvailability(ctx context.Context, raw, forMentorID string) (*AvailabilityResult, error) {
	username := slug.NormalizeUsername(raw)
	result := &AvailabilityResult{Username: username}

	if err := slug.ValidateUsername(username); err != nil {
		recordUsernameRejection(usernameSurfaceAvailability, err)
		result.Reason = "invalid"
		if errors.Is(err, slug.ErrUsernameReserved) {
			result.Reason = "reserved"
		}
		result.Message = err.Error()
		return result, nil
	}

	// The mentor's own current slug counts as available (a no-op change).
	if forMentorID != "" {
		mentor, err := s.repo.GetByMentorId(ctx, forMentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
		if err == nil && mentor != nil && mentor.Slug == username {
			result.Available = true
			return result, nil
		}
	}

	taken, err := s.repo.IsSlugTaken(ctx, username, forMentorID)
	if err != nil {
		return nil, err
	}
	if taken {
		result.Reason = "taken"
		result.Message = "this username is already taken"
		return result, nil
	}

	result.Available = true
	return result, nil
}

// Status returns the mentor's current username and whether the 14-day
// cooldown currently allows a change.
func (s *UsernameService) Status(ctx context.Context, mentorID string) (*UsernameStatus, error) {
	mentor, err := s.repo.GetByMentorId(ctx, mentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
	if err != nil {
		return nil, err
	}

	status := &UsernameStatus{Username: mentor.Slug, CanChange: true}
	lastChange, err := s.repo.LatestMentorSlugChange(ctx, mentorID)
	if err != nil {
		return nil, err
	}
	if lastChange != nil {
		next := lastChange.Add(UsernameChangeCooldown)
		if s.now().Before(next) {
			status.CanChange = false
			status.NextChangeAt = &next
		}
	}
	return status, nil
}

// Change renames the mentor. changedBy is "mentor" (cooldown enforced) or
// "admin" (no cooldown; only availability is checked). Returns the new
// normalized username. Error cases: validation errors from pkg/slug,
// ErrUsernameTaken, *CooldownError.
func (s *UsernameService) Change(ctx context.Context, mentorID, raw, changedBy string) (string, error) {
	username := slug.NormalizeUsername(raw)
	if err := slug.ValidateUsername(username); err != nil {
		recordUsernameRejection(usernameSurfaceChange, err)
		return "", err
	}

	// No-op fast path: submitting the current username is always allowed and
	// never consumes the cooldown (this mirrors ChangeSlug's in-tx no-op).
	// Detect it BEFORE the cooldown pre-check, otherwise a mentor in cooldown
	// who re-submits their own name — which availability reports as available —
	// would get a spurious 429.
	current, err := s.repo.GetByMentorId(ctx, mentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
	if err != nil {
		return "", err
	}
	if current != nil && current.Slug == username {
		return username, nil
	}

	// Cooldown applies only to mentor-initiated changes. This pre-check is a
	// fast path (avoids opening a transaction for the common case); the
	// authoritative, race-free check runs inside ChangeSlug's transaction.
	var cooldown time.Duration
	if changedBy == "mentor" {
		cooldown = UsernameChangeCooldown
		lastChange, err := s.repo.LatestMentorSlugChange(ctx, mentorID)
		if err != nil {
			return "", err
		}
		if lastChange != nil {
			next := lastChange.Add(UsernameChangeCooldown)
			if s.now().Before(next) {
				return "", &CooldownError{NextChangeAt: next}
			}
		}
	}

	oldSlug, err := s.repo.ChangeSlug(ctx, mentorID, username, changedBy, cooldown, s.now())
	if err != nil {
		if errors.Is(err, repository.ErrSlugTaken) {
			return "", ErrUsernameTaken
		}
		var rce *repository.CooldownError
		if errors.As(err, &rce) {
			return "", &CooldownError{NextChangeAt: rce.NextChangeAt}
		}
		return "", err
	}

	if oldSlug != username {
		s.tracker.Track(ctx, analytics.EventMentorUsernameChanged, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id":  mentorID,
			"changed_by": changedBy,
		})
	}
	return username, nil
}
