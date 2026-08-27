package services_test

// The counter behind D87: without it production cannot tell a rule that never
// fires from one that is turning real people away. These tests pin the label
// values end to end, through the service, not through the mapping helper.

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

func rejectionCount(t *testing.T, surface, reason string) float64 {
	t.Helper()
	counter, err := metrics.UsernameRejections.GetMetricWithLabelValues(surface, reason)
	if err != nil {
		t.Fatalf("counter %s/%s: %v", surface, reason, err)
	}
	return testutil.ToFloat64(counter)
}

// ErrUsernameMentorDerivative wraps ErrUsernameReserved. Mapped in the wrong
// order every derivative counts as a plain reserved word and the rule the
// counter exists to measure becomes invisible.
func TestUsernameRejection_DerivativeIsNotCountedAsReserved(t *testing.T) {
	repo := &usernameMockRepo{}
	svc := newUsernameService(repo, time.Now())

	beforeDerivative := rejectionCount(t, "change", "mentor_derivative")
	beforeReserved := rejectionCount(t, "change", "reserved")

	if _, err := svc.Change(context.Background(), "mentor-1", "anna-mentor", "mentor"); err == nil {
		t.Fatal("expected the change to be refused")
	}

	if got := rejectionCount(t, "change", "mentor_derivative") - beforeDerivative; got != 1 {
		t.Errorf("mentor_derivative counter moved by %v, want 1", got)
	}
	if got := rejectionCount(t, "change", "reserved") - beforeReserved; got != 0 {
		t.Errorf("reserved counter moved by %v, want 0 — the wrapped sentinel was matched first", got)
	}
}

// A plain reserved word must still land on "reserved", or the two rules become
// indistinguishable from the other direction.
func TestUsernameRejection_PlainReservedWordKeepsItsReason(t *testing.T) {
	repo := &usernameMockRepo{}
	svc := newUsernameService(repo, time.Now())

	before := rejectionCount(t, "change", "reserved")
	beforeDerivative := rejectionCount(t, "change", "mentor_derivative")

	if _, err := svc.Change(context.Background(), "mentor-1", "admin", "admin"); err == nil {
		t.Fatal("expected the change to be refused")
	}

	if got := rejectionCount(t, "change", "reserved") - before; got != 1 {
		t.Errorf("reserved counter moved by %v, want 1", got)
	}
	if got := rejectionCount(t, "change", "mentor_derivative") - beforeDerivative; got != 0 {
		t.Errorf("mentor_derivative counter moved by %v, want 0", got)
	}
}

// The availability check answers the caller and returns; the counter is the
// only record that it refused anything.
func TestUsernameRejection_AvailabilityIsCountedOnItsOwnSurface(t *testing.T) {
	repo := &usernameMockRepo{}
	svc := newUsernameService(repo, time.Now())

	before := rejectionCount(t, "availability", "mentor_derivative")

	res, err := svc.CheckAvailability(context.Background(), "topmentor", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Available {
		t.Fatal("expected the candidate to be unavailable")
	}

	if got := rejectionCount(t, "availability", "mentor_derivative") - before; got != 1 {
		t.Errorf("availability counter moved by %v, want 1", got)
	}
}

// A no-op (the mentor re-submitting their own current username) is not a
// rejection and must not inflate the counter.
func TestUsernameRejection_ValidUsernameCountsNothing(t *testing.T) {
	repo := &usernameMockRepo{mentor: &models.Mentor{Slug: "anna-smith"}, changeOld: "old"}
	svc := newUsernameService(repo, time.Now())

	before := rejectionCount(t, "change", "mentor_derivative")
	beforeFormat := rejectionCount(t, "change", "bad_format")

	if _, err := svc.Change(context.Background(), "mentor-1", "anna-smith", "mentor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := rejectionCount(t, "change", "mentor_derivative") - before; got != 0 {
		t.Errorf("mentor_derivative counter moved by %v on a valid username, want 0", got)
	}
	if got := rejectionCount(t, "change", "bad_format") - beforeFormat; got != 0 {
		t.Errorf("bad_format counter moved by %v on a valid username, want 0", got)
	}
}
