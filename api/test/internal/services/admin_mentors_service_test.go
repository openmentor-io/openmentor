package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// adminMockRepo implements services.AdminMentorsRepository for the
// moderation action tests.
type adminMockRepo struct {
	mentor *models.AdminMentorDetails
	getErr error

	approveCalled  bool
	returnCalled   bool
	returnedNote   string
	setStatusCalls map[string]string // mentorID -> status
	returnErr      error
	approveErr     error
}

func (m *adminMockRepo) ListForModeration(ctx context.Context, statuses []string) ([]models.AdminMentorListItem, error) {
	return nil, nil
}

func (m *adminMockRepo) GetForModerationByID(ctx context.Context, mentorID string) (*models.AdminMentorDetails, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.mentor, nil
}

func (m *adminMockRepo) GetTagIDByName(ctx context.Context, tagName string) (string, error) {
	return "tag-id", nil
}

func (m *adminMockRepo) Update(ctx context.Context, mentorID string, updates map[string]interface{}) error {
	return nil
}

func (m *adminMockRepo) UpdateMentorTags(ctx context.Context, mentorID string, tagIDs []string) error {
	return nil
}

func (m *adminMockRepo) SetMentorStatus(ctx context.Context, mentorID, status string) error {
	if m.setStatusCalls == nil {
		m.setStatusCalls = map[string]string{}
	}
	m.setStatusCalls[mentorID] = status
	return nil
}

func (m *adminMockRepo) ApproveMentorModeration(ctx context.Context, mentorID string) error {
	m.approveCalled = true
	return m.approveErr
}

func (m *adminMockRepo) ReturnMentorToDraft(ctx context.Context, mentorID, note string) error {
	if m.returnErr != nil {
		return m.returnErr
	}
	m.returnCalled = true
	m.returnedNote = note
	return nil
}

var _ services.AdminMentorsRepository = (*adminMockRepo)(nil)

func newAdminTestService(repo *adminMockRepo, tracker *capturingTracker) *services.AdminMentorsService {
	return services.NewAdminMentorsService(repo, nil, &config.Config{}, nil, tracker)
}

func adminSession(role models.ModeratorRole) *models.AdminSession {
	return &models.AdminSession{ModeratorID: "mod-1", Role: role}
}

func pendingMentorDetails() *models.AdminMentorDetails {
	return &models.AdminMentorDetails{MentorID: "mentor-1", Status: "pending"}
}

func TestAdminMentorsService_ReturnMentor_Success(t *testing.T) {
	repo := &adminMockRepo{mentor: pendingMentorDetails()}
	tracker := &capturingTracker{}
	svc := newAdminTestService(repo, tracker)

	_, err := svc.ReturnMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "  Please add a real photo.  ")
	if err != nil {
		t.Fatalf("ReturnMentor() error = %v", err)
	}
	if !repo.returnCalled {
		t.Error("expected ReturnMentorToDraft to be called")
	}
	if repo.returnedNote != "Please add a real photo." {
		t.Errorf("expected trimmed note, got %q", repo.returnedNote)
	}

	event := tracker.last()
	if event == nil || event.event != "admin_mentor_returned" {
		t.Fatalf("expected admin_mentor_returned event, got %+v", event)
	}
	if event.properties["outcome"] != "success" {
		t.Errorf("expected success outcome, got %v", event.properties["outcome"])
	}
}

func TestAdminMentorsService_ReturnMentor_ReasonValidation(t *testing.T) {
	tests := []struct {
		name   string
		reason string
	}{
		{"empty reason", ""},
		{"whitespace-only reason", "   \n\t "},
		{"reason too long", strings.Repeat("x", 2001)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &adminMockRepo{mentor: pendingMentorDetails()}
			svc := newAdminTestService(repo, &capturingTracker{})

			_, err := svc.ReturnMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", tt.reason)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if repo.returnCalled {
				t.Error("ReturnMentorToDraft must not be called on invalid reason")
			}
		})
	}
}

func TestAdminMentorsService_ReturnMentor_OnlyFromPending(t *testing.T) {
	for _, status := range []string{"draft", "active", "inactive", "declined"} {
		t.Run(status, func(t *testing.T) {
			repo := &adminMockRepo{mentor: &models.AdminMentorDetails{MentorID: "mentor-1", Status: status}}
			svc := newAdminTestService(repo, &capturingTracker{})

			_, err := svc.ReturnMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "reason")
			if err == nil {
				t.Fatalf("expected error returning mentor with status %s", status)
			}
			if repo.returnCalled {
				t.Error("ReturnMentorToDraft must not be called")
			}
		})
	}
}

func TestAdminMentorsService_ReturnMentor_ActivatedGuard(t *testing.T) {
	// HARD GUARD: a mentor that has ever been active (activated_at set)
	// can never be returned to draft, even if currently pending.
	activatedAt := time.Now().Add(-24 * time.Hour)
	repo := &adminMockRepo{mentor: &models.AdminMentorDetails{
		MentorID:    "mentor-1",
		Status:      "pending",
		ActivatedAt: &activatedAt,
	}}
	tracker := &capturingTracker{}
	svc := newAdminTestService(repo, tracker)

	_, err := svc.ReturnMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "reason")
	if err != services.ErrMentorAlreadyActivated {
		t.Fatalf("expected ErrMentorAlreadyActivated, got %v", err)
	}
	if repo.returnCalled {
		t.Error("ReturnMentorToDraft must not be called for an activated mentor")
	}

	event := tracker.last()
	if event == nil || event.properties["outcome"] != "forbidden_already_activated" {
		t.Errorf("expected forbidden_already_activated outcome, got %+v", event)
	}
}

func TestAdminMentorsService_ReturnMentor_ModeratorRoleRestrictedToPending(t *testing.T) {
	// A moderator (non-admin) may only act on pending mentors; the service
	// GetMentor gate rejects other statuses before validation runs.
	repo := &adminMockRepo{mentor: &models.AdminMentorDetails{MentorID: "mentor-1", Status: "active"}}
	svc := newAdminTestService(repo, &capturingTracker{})

	_, err := svc.ReturnMentor(context.Background(), adminSession(models.ModeratorRoleModerator), "mentor-1", "reason")
	if err != services.ErrAdminForbiddenAction {
		t.Fatalf("expected ErrAdminForbiddenAction, got %v", err)
	}
}

func TestAdminMentorsService_ApproveMentor_UsesApproveModeration(t *testing.T) {
	// Approve must go through ApproveMentorModeration (stamps activated_at,
	// clears moderation_note) instead of a plain status write.
	repo := &adminMockRepo{mentor: pendingMentorDetails()}
	svc := newAdminTestService(repo, &capturingTracker{})

	_, err := svc.ApproveMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1")
	if err != nil {
		t.Fatalf("ApproveMentor() error = %v", err)
	}
	if !repo.approveCalled {
		t.Error("expected ApproveMentorModeration to be called")
	}
	if len(repo.setStatusCalls) != 0 {
		t.Errorf("expected no plain SetMentorStatus calls, got %v", repo.setStatusCalls)
	}
}

func TestAdminMentorsService_DeclineMentor_PlainStatusWrite(t *testing.T) {
	repo := &adminMockRepo{mentor: pendingMentorDetails()}
	svc := newAdminTestService(repo, &capturingTracker{})

	_, err := svc.DeclineMentor(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1")
	if err != nil {
		t.Fatalf("DeclineMentor() error = %v", err)
	}
	if repo.setStatusCalls["mentor-1"] != "declined" {
		t.Errorf("expected declined status write, got %v", repo.setStatusCalls)
	}
	if repo.approveCalled {
		t.Error("ApproveMentorModeration must not be called on decline")
	}
}
