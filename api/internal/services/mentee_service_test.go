package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
)

type fakeMenteeRepo struct {
	rows []repository.MenteeRequestRow
}

func (f *fakeMenteeRepo) SetLoginToken(context.Context, string, string, time.Time) (string, error) {
	return "", repository.ErrMenteeHasNoRequests
}
func (f *fakeMenteeRepo) ConsumeLoginToken(context.Context, string) (*repository.ConsumedMenteeLogin, error) {
	return nil, repository.ErrTokenNotConsumable
}
func (f *fakeMenteeRepo) BumpMenteeSessionVersion(context.Context, string) error { return nil }
func (f *fakeMenteeRepo) ListRequests(context.Context, string) ([]repository.MenteeRequestRow, error) {
	return f.rows, nil
}

func menteeRow(id string, status models.RequestStatus) repository.MenteeRequestRow {
	return repository.MenteeRequestRow{
		ID:                id,
		Status:            status,
		Details:           "help me",
		CreatedAt:         time.Now(),
		DeclineReason:     "no_time",
		DeclineComment:    "busy month",
		MentorName:        "Alice Mentor",
		MentorSlug:        "alice",
		MentorStatus:      "active",
		MentorCalendarURL: "https://cal.example.com/alice",
		MentorAvailable:   true,
	}
}

func listMentee(t *testing.T, rows ...repository.MenteeRequestRow) map[string]models.MenteeRequest {
	t.Helper()
	svc := NewMenteeService(&fakeMenteeRepo{rows: rows}, &config.Config{}, nil, nil)
	resp, err := svc.ListRequests(context.Background(), "mentee-1")
	require.NoError(t, err)
	out := map[string]models.MenteeRequest{}
	for _, r := range resp.Requests {
		out[r.ID] = r
	}
	return out
}

// TestMenteeSeesTheCalendarOnlyWhileTheRequestIsOpen pins the visibility rule
// server-side, so a UI change cannot widen it: the mentor's calendar link is the
// only mentor contact a mentee is shown, and only while there is something to
// book.
func TestMenteeSeesTheCalendarOnlyWhileTheRequestIsOpen(t *testing.T) {
	got := listMentee(t,
		menteeRow("pending", models.StatusPending),
		menteeRow("contacted", models.StatusContacted),
		menteeRow("working", models.StatusWorking),
		menteeRow("reschedule", models.StatusReschedule),
		menteeRow("done", models.StatusDone),
		menteeRow("declined", models.StatusDeclined),
		menteeRow("unavailable", models.StatusUnavailable),
	)

	want := map[string]models.MenteeRequestGroup{
		"pending": models.MenteeGroupPending, "contacted": models.MenteeGroupActive,
		"working": models.MenteeGroupActive, "reschedule": models.MenteeGroupActive,
		"done": models.MenteeGroupDone, "declined": models.MenteeGroupClosed,
		"unavailable": models.MenteeGroupClosed,
	}
	for id, group := range want {
		req := got[id]
		assert.Equal(t, group, req.Group, id)
		open := group == models.MenteeGroupPending || group == models.MenteeGroupActive
		if open {
			assert.Equal(t, "https://cal.example.com/alice", req.Mentor.CalendarURL, id)
		} else {
			assert.Empty(t, req.Mentor.CalendarURL, "%s must not expose the calendar", id)
		}
		assert.Equal(t, id == "done", req.CanReview, id)
		assert.Equal(t, id == "declined", req.DeclineReason != "", "decline reason only on a declined request (%s)", id)
	}
}

func TestMenteeDashboardHidesWhatNoLongerExists(t *testing.T) {
	inactive := menteeRow("inactive", models.StatusPending)
	inactive.MentorStatus = "inactive"

	deleted := menteeRow("deleted", models.StatusDone)
	deleted.MentorAvailable = false

	reviewed := menteeRow("reviewed", models.StatusDone)
	at := time.Now()
	reviewed.ReviewCreatedAt = &at
	reviewed.MentorReview = "great"

	got := listMentee(t, inactive, deleted, reviewed)

	assert.Empty(t, got["inactive"].Mentor.Slug, "no link to a profile page that is not public")
	assert.Equal(t, "https://cal.example.com/alice", got["inactive"].Mentor.CalendarURL)

	assert.False(t, got["deleted"].Mentor.Available)
	assert.Empty(t, got["deleted"].Mentor.Name, "a deleted profile's name is not shown (D70)")
	assert.False(t, got["deleted"].CanReview)

	require.NotNil(t, got["reviewed"].Review)
	assert.Equal(t, "great", got["reviewed"].Review.MentorReview)
	assert.False(t, got["reviewed"].CanReview, "one review per request; it is shown, not editable")
}
