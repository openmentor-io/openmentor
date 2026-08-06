package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// ---------------------------------------------------------------------------
// Mentor deleting their OWN profile
// ---------------------------------------------------------------------------

func deletionService(repo *statusMockRepo, tracker *capturingTracker) *services.ProfileService {
	return services.NewProfileService(repo, nil, &config.Config{}, nil, tracker)
}

func liveMentor() *models.Mentor {
	return &models.Mentor{MentorID: "mentor-1", Slug: "anna-petrova-42", Status: "active"}
}

func TestDeleteProfile_SucceedsWhenUsernameMatches(t *testing.T) {
	repo := &statusMockRepo{mentor: liveMentor(), softDeleteRevoked: 2}
	tracker := &capturingTracker{}
	svc := deletionService(repo, tracker)

	if err := svc.DeleteProfileByMentorId(context.Background(), "mentor-1", "anna-petrova-42"); err != nil {
		t.Fatalf("DeleteProfileByMentorId() error = %v", err)
	}
	if !repo.softDeleteCalled {
		t.Fatal("expected SoftDeleteMentor to be called")
	}
	if repo.softDeleteMentorID != "mentor-1" {
		t.Errorf("deleted mentor %q, want mentor-1", repo.softDeleteMentorID)
	}

	event := tracker.last()
	if event == nil || event.event != "mentor_profile_deleted" {
		t.Fatalf("expected mentor_profile_deleted event, got %+v", event)
	}
	if event.properties["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", event.properties["outcome"])
	}
	if event.properties["invitations_revoked"] != 2 {
		t.Errorf("invitations_revoked = %v, want 2", event.properties["invitations_revoked"])
	}
}

// The typed confirmation is the whole safety mechanism, so a wrong value must
// stop the delete before it reaches the database.
func TestDeleteProfile_RejectsWrongUsername(t *testing.T) {
	for _, typed := range []string{"", "someone-else", "anna-petrova", "anna petrova 42"} {
		repo := &statusMockRepo{mentor: liveMentor()}
		svc := deletionService(repo, &capturingTracker{})

		err := svc.DeleteProfileByMentorId(context.Background(), "mentor-1", typed)
		if !errors.Is(err, services.ErrDeleteConfirmationMismatch) {
			t.Errorf("typed %q: error = %v, want ErrDeleteConfirmationMismatch", typed, err)
		}
		if repo.softDeleteCalled {
			t.Errorf("typed %q: SoftDeleteMentor must not be called on a mismatch", typed)
		}
	}
}

// Slugs are lowercase by construction, so a mentor who capitalises their own
// name (or whose phone does) is confirming correctly.
func TestDeleteProfile_ConfirmationIsCaseInsensitiveAndTrimmed(t *testing.T) {
	for _, typed := range []string{"Anna-Petrova-42", "  anna-petrova-42  ", "ANNA-PETROVA-42"} {
		repo := &statusMockRepo{mentor: liveMentor()}
		svc := deletionService(repo, &capturingTracker{})

		if err := svc.DeleteProfileByMentorId(context.Background(), "mentor-1", typed); err != nil {
			t.Errorf("typed %q: error = %v, want success", typed, err)
		}
		if !repo.softDeleteCalled {
			t.Errorf("typed %q: expected the delete to go through", typed)
		}
	}
}

func TestDeleteProfile_AlreadyDeletedIsReportedAsSuch(t *testing.T) {
	deletedAt := time.Now()
	mentor := liveMentor()
	mentor.DeletedAt = &deletedAt
	repo := &statusMockRepo{mentor: mentor}
	svc := deletionService(repo, &capturingTracker{})

	err := svc.DeleteProfileByMentorId(context.Background(), "mentor-1", "anna-petrova-42")
	if !errors.Is(err, services.ErrProfileAlreadyDeleted) {
		t.Fatalf("error = %v, want ErrProfileAlreadyDeleted", err)
	}
	if repo.softDeleteCalled {
		t.Error("SoftDeleteMentor must not be called for an already-deleted profile")
	}
}

// The repository's compare-and-swap is what makes the delete exactly-once; the
// service must translate its verdict rather than report a server error.
func TestDeleteProfile_TranslatesConcurrentDelete(t *testing.T) {
	repo := &statusMockRepo{mentor: liveMentor(), softDeleteErr: repository.ErrMentorAlreadyDeleted}
	svc := deletionService(repo, &capturingTracker{})

	err := svc.DeleteProfileByMentorId(context.Background(), "mentor-1", "anna-petrova-42")
	if !errors.Is(err, services.ErrProfileAlreadyDeleted) {
		t.Fatalf("error = %v, want ErrProfileAlreadyDeleted", err)
	}
}

// ---------------------------------------------------------------------------
// Admin-side deletion and restore
// ---------------------------------------------------------------------------

func deletedMentorDetails() *models.AdminMentorDetails {
	deletedAt := time.Now()
	return &models.AdminMentorDetails{
		MentorID:  "mentor-1",
		Slug:      "anna-petrova-42",
		Status:    "inactive",
		DeletedAt: &deletedAt,
	}
}

func liveMentorDetails() *models.AdminMentorDetails {
	return &models.AdminMentorDetails{MentorID: "mentor-1", Slug: "anna-petrova-42", Status: "active"}
}

func TestAdminDeleteMentor_RequiresAdminRole(t *testing.T) {
	repo := &adminMockRepo{mentor: liveMentorDetails()}
	svc := newAdminTestService(repo, &capturingTracker{})

	_, err := svc.DeleteMentor(context.Background(), adminSession(models.ModeratorRoleModerator), "mentor-1", "anna-petrova-42")
	if !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Fatalf("error = %v, want ErrAdminForbiddenAction", err)
	}
	if repo.softDeleteCalled {
		t.Error("a moderator must not be able to delete a profile")
	}
}

func TestAdminDeleteMentor_RequiresMatchingUsername(t *testing.T) {
	repo := &adminMockRepo{mentor: liveMentorDetails()}
	svc := newAdminTestService(repo, &capturingTracker{})

	_, err := svc.DeleteMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "the-wrong-mentor")
	if !errors.Is(err, services.ErrDeleteConfirmationMismatch) {
		t.Fatalf("error = %v, want ErrDeleteConfirmationMismatch", err)
	}
	if repo.softDeleteCalled {
		t.Error("SoftDeleteMentor must not be called on a mismatch")
	}
}

func TestAdminRestoreMentor_RequiresAdminRole(t *testing.T) {
	repo := &adminMockRepo{mentor: deletedMentorDetails()}
	svc := newAdminTestService(repo, &capturingTracker{})

	_, err := svc.RestoreMentor(context.Background(), adminSession(models.ModeratorRoleModerator), "mentor-1")
	if !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Fatalf("error = %v, want ErrAdminForbiddenAction", err)
	}
	if repo.restoreCalled {
		t.Error("a moderator must not be able to restore a profile")
	}
}

func TestAdminRestoreMentor_Success(t *testing.T) {
	repo := &adminMockRepo{mentor: deletedMentorDetails()}
	tracker := &capturingTracker{}
	svc := newAdminTestService(repo, tracker)

	if _, err := svc.RestoreMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1"); err != nil {
		t.Fatalf("RestoreMentor() error = %v", err)
	}
	if !repo.restoreCalled {
		t.Fatal("expected RestoreMentor to be called")
	}
	event := tracker.last()
	if event == nil || event.event != "admin_mentor_restored" {
		t.Fatalf("expected admin_mentor_restored event, got %+v", event)
	}
}

// "Once the profile has this deleted status, it immediately becomes
// unavailable for any action." Every moderation action must refuse.
func TestAdminActions_RefuseOnDeletedProfile(t *testing.T) {
	session := adminSession(models.ModeratorRoleAdmin)
	actions := map[string]func(*services.AdminMentorsService) error{
		"approve": func(s *services.AdminMentorsService) error {
			_, err := s.ApproveMentor(context.Background(), session, "mentor-1")
			return err
		},
		"decline": func(s *services.AdminMentorsService) error {
			_, err := s.DeclineMentor(context.Background(), session, "mentor-1")
			return err
		},
		"return": func(s *services.AdminMentorsService) error {
			_, err := s.ReturnMentor(context.Background(), session, "mentor-1", "please fix your photo")
			return err
		},
		"status toggle": func(s *services.AdminMentorsService) error {
			_, err := s.UpdateMentorStatus(context.Background(), session, "mentor-1", "active")
			return err
		},
		"profile update": func(s *services.AdminMentorsService) error {
			_, err := s.UpdateMentorProfile(context.Background(), session, "mentor-1",
				&models.AdminMentorProfileUpdateRequest{Name: "Anna", Tags: []string{"go"}})
			return err
		},
	}

	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			repo := &adminMockRepo{mentor: deletedMentorDetails()}
			svc := newAdminTestService(repo, &capturingTracker{})

			err := action(svc)
			if !errors.Is(err, services.ErrMentorDeleted) {
				t.Fatalf("error = %v, want ErrMentorDeleted", err)
			}
			if repo.approveCalled || repo.returnCalled || len(repo.setStatusCalls) > 0 {
				t.Error("no write may reach the repository for a deleted profile")
			}
		})
	}
}

// Only an admin may see a deleted profile's details.
func TestAdminGetMentor_HidesDeletedProfileFromModerators(t *testing.T) {
	repo := &adminMockRepo{mentor: deletedMentorDetails()}
	svc := newAdminTestService(repo, &capturingTracker{})

	if _, err := svc.GetMentor(context.Background(), adminSession(models.ModeratorRoleModerator), "mentor-1"); !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Fatalf("moderator: error = %v, want ErrAdminForbiddenAction", err)
	}
	if _, err := svc.GetMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1"); err != nil {
		t.Fatalf("admin: error = %v, want the details", err)
	}
}

// The deleted tab is admin-only and does not go through the status groups.
func TestAdminListMentors_DeletedTab(t *testing.T) {
	repo := &adminMockRepo{deletedList: []models.AdminMentorListItem{{MentorID: "mentor-1"}}}
	svc := newAdminTestService(repo, &capturingTracker{})

	if _, err := svc.ListMentors(context.Background(), adminSession(models.ModeratorRoleModerator), models.MentorModerationFilterDeleted); !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Fatalf("moderator: error = %v, want ErrAdminForbiddenAction", err)
	}
	if repo.listDeletedCalled {
		t.Fatal("a moderator's request must not reach the deleted listing")
	}

	got, err := svc.ListMentors(context.Background(), adminSession(models.ModeratorRoleAdmin), models.MentorModerationFilterDeleted)
	if err != nil {
		t.Fatalf("admin: error = %v", err)
	}
	if !repo.listDeletedCalled {
		t.Error("expected ListDeletedForModeration to be used for the deleted tab")
	}
	if len(got) != 1 {
		t.Errorf("got %d mentors, want 1", len(got))
	}
	// The status tabs must not have been consulted: deletion is a timestamp,
	// not a status group.
	if len(repo.listForModCalls) != 0 {
		t.Errorf("status listing called with %v, want no call", repo.listForModCalls)
	}
}
