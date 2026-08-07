package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/internal/services"
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
)

// The mentor's own profile save (C1). The transactional guarantee itself is a SQL
// property and lives in api/internal/repository/profile_write_atomicity_db_test.go;
// what these tests pin is the service's half of it — that a failed write is
// REPORTED, that the calendar link is always part of the write, and that a save
// racing a deletion answers "already deleted" instead of a 500.

func saveProfileFixture() *models.SaveProfileRequest {
	return &models.SaveProfileRequest{
		Name:         "Mentor Name",
		Job:          "Staff Engineer",
		Workplace:    "OpenMentor",
		Experience:   "5-10",
		Price:        "free",
		Tags:         []string{"Backend", "System Design"},
		Description:  "short description",
		About:        "about text",
		Competencies: "competencies text",
	}
}

func newSaveTestService(repo *statusMockRepo, tracker *capturingTracker) *services.ProfileService {
	return services.NewProfileService(repo, nil, &config.Config{}, nil, tracker)
}

// TestSaveProfileReportsAFailedWrite is the C1 defect in test form: the tag write
// was a second operation whose failure was logged while the save still returned
// nil, so the mentor saw "saved" over a profile whose tags had not changed.
func TestSaveProfileReportsAFailedWrite(t *testing.T) {
	repo := &statusMockRepo{
		mentor:          &models.Mentor{MentorID: "mentor-1", Status: "active"},
		profileWriteErr: errors.New("tag insert failed"),
	}
	svc := newSaveTestService(repo, &capturingTracker{})

	err := svc.SaveProfileByMentorId(context.Background(), "mentor-1", saveProfileFixture())
	if err == nil {
		t.Fatal("SaveProfileByMentorId() = nil, want an error: nothing was written")
	}
	if repo.profileWriteCalls != 1 {
		t.Errorf("profile write calls = %d, want 1 (row and tags go together)", repo.profileWriteCalls)
	}
}

// TestSaveProfileWritesTheCalendarURLUnconditionally: `if req.CalendarURL != ""`
// meant a mentor could add a calendar link through the portal but never remove
// one, while the admin form always wrote the column — so an empty value meant two
// different things depending on which form sent it.
func TestSaveProfileWritesTheCalendarURLUnconditionally(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "active"}}
	svc := newSaveTestService(repo, &capturingTracker{})

	req := saveProfileFixture()
	req.CalendarURL = "" // the mentor cleared the field

	if err := svc.SaveProfileByMentorId(context.Background(), "mentor-1", req); err != nil {
		t.Fatalf("SaveProfileByMentorId() error = %v", err)
	}

	value, present := repo.profileUpdates["calendar_url"]
	if !present {
		t.Fatal("calendar_url missing from the update: a cleared link can never be removed")
	}
	if value != "" {
		t.Errorf("calendar_url = %q, want the empty value the mentor submitted", value)
	}
}

// TestSaveProfileOnADeletedProfileIsAConflict: the existence check happens before
// the write, so a deletion committing in between is reported by the write itself
// (ErrMentorNotWritable). The mentor gets "already deleted", not a 500.
func TestSaveProfileOnADeletedProfileIsAConflict(t *testing.T) {
	repo := &statusMockRepo{
		mentor:          &models.Mentor{MentorID: "mentor-1", Status: "active"},
		profileWriteErr: repository.ErrMentorNotWritable,
	}
	tracker := &capturingTracker{}
	svc := newSaveTestService(repo, tracker)

	err := svc.SaveProfileByMentorId(context.Background(), "mentor-1", saveProfileFixture())
	if !errors.Is(err, services.ErrProfileAlreadyDeleted) {
		t.Fatalf("SaveProfileByMentorId() error = %v, want ErrProfileAlreadyDeleted", err)
	}
	if last := tracker.last(); last == nil || last.properties["outcome"] != "already_deleted" {
		t.Errorf("tracked outcome = %v, want already_deleted", last)
	}
}

// TestSaveProfileRefusesUnknownTagsWithoutWriting keeps the strict resolution
// honest now that the write is one call: an unknown tag must stop the save BEFORE
// it, because the tag set is replaced wholesale and a partial match would shrink
// the mentor's profile while reporting success.
func TestSaveProfileRefusesUnknownTagsWithoutWriting(t *testing.T) {
	repo := &statusMockRepo{
		mentor:      &models.Mentor{MentorID: "mentor-1", Status: "active"},
		unknownTags: map[string]bool{"System Design": true},
	}
	svc := newSaveTestService(repo, &capturingTracker{})

	err := svc.SaveProfileByMentorId(context.Background(), "mentor-1", saveProfileFixture())
	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("SaveProfileByMentorId() error = %v, want an invalid-input error", err)
	}
	if repo.profileWriteCalls != 0 {
		t.Errorf("profile write calls = %d, want 0: a rejected save writes nothing", repo.profileWriteCalls)
	}
}
