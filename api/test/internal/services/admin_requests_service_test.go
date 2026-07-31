package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// adminRequestsMockRepo implements services.AdminRequestsRepository.
type adminRequestsMockRepo struct {
	request  *models.MentorClientRequest
	list     []*models.MentorClientRequest
	getErr   error
	listErr  error
	updErr   error
	listArgs []models.RequestStatus

	updatedTo     models.RequestStatus
	updateCalled  bool
	getByIDCalled int
}

func (m *adminRequestsMockRepo) ListByMentorFiltered(ctx context.Context, mentorID string, statuses []models.RequestStatus) ([]*models.MentorClientRequest, error) {
	m.listArgs = statuses
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.list, nil
}

func (m *adminRequestsMockRepo) GetByID(ctx context.Context, id string) (*models.MentorClientRequest, error) {
	m.getByIDCalled++
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.request, nil
}

func (m *adminRequestsMockRepo) UpdateStatus(ctx context.Context, id string, status models.RequestStatus) error {
	if m.updErr != nil {
		return m.updErr
	}
	m.updateCalled = true
	m.updatedTo = status
	// Reflect the write so the service's re-fetch returns the new state.
	if m.request != nil {
		m.request.Status = status
	}
	return nil
}

var _ services.AdminRequestsRepository = (*adminRequestsMockRepo)(nil)

func requestOf(status models.RequestStatus) *models.MentorClientRequest {
	return &models.MentorClientRequest{ID: "req-1", MentorID: "mentor-1", Status: status}
}

// TestAdminRequestsService_UpdateRequestStatus_FromTerminal is the point of the
// whole endpoint: an admin can move a request back out of a terminal status,
// which the mentor-facing flow rejects.
func TestAdminRequestsService_UpdateRequestStatus_FromTerminal(t *testing.T) {
	terminalStatuses := []models.RequestStatus{
		models.StatusDone,
		models.StatusDeclined,
		models.StatusUnavailable,
	}

	for _, from := range terminalStatuses {
		t.Run(string(from), func(t *testing.T) {
			repo := &adminRequestsMockRepo{request: requestOf(from)}
			tracker := &capturingTracker{}
			svc := services.NewAdminRequestsService(repo, tracker)

			updated, err := svc.UpdateRequestStatus(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "req-1", models.StatusWorking)
			if err != nil {
				t.Fatalf("UpdateRequestStatus() error = %v", err)
			}
			if !repo.updateCalled || repo.updatedTo != models.StatusWorking {
				t.Errorf("expected status written as working, got called=%v to=%q", repo.updateCalled, repo.updatedTo)
			}
			if updated.Status != models.StatusWorking {
				t.Errorf("expected returned request in working, got %q", updated.Status)
			}

			event := tracker.last()
			if event == nil || event.event != "admin_request_status_updated" {
				t.Fatalf("expected admin_request_status_updated event, got %+v", event)
			}
			if event.properties["outcome"] != "success" {
				t.Errorf("expected success outcome, got %v", event.properties["outcome"])
			}
			if event.properties["from_status"] != string(from) {
				t.Errorf("expected from_status %q, got %v", from, event.properties["from_status"])
			}
		})
	}
}

func TestAdminRequestsService_UpdateRequestStatus_InvalidStatus(t *testing.T) {
	repo := &adminRequestsMockRepo{request: requestOf(models.StatusPending)}
	svc := services.NewAdminRequestsService(repo, &capturingTracker{})

	_, err := svc.UpdateRequestStatus(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "req-1", models.RequestStatus("archived"))
	if err == nil {
		t.Fatal("expected error for unknown status, got nil")
	}
	if repo.updateCalled {
		t.Error("UpdateStatus must not be called for an unknown status")
	}
}

func TestAdminRequestsService_UpdateRequestStatus_NoChangeSkipsWrite(t *testing.T) {
	repo := &adminRequestsMockRepo{request: requestOf(models.StatusDone)}
	tracker := &capturingTracker{}
	svc := services.NewAdminRequestsService(repo, tracker)

	if _, err := svc.UpdateRequestStatus(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "req-1", models.StatusDone); err != nil {
		t.Fatalf("UpdateRequestStatus() error = %v", err)
	}
	if repo.updateCalled {
		t.Error("UpdateStatus must not be called when the status is unchanged")
	}
	if event := tracker.last(); event == nil || event.properties["outcome"] != "no_change" {
		t.Errorf("expected no_change outcome, got %+v", event)
	}
}

func TestAdminRequestsService_UpdateRequestStatus_MentorMismatchIsNotFound(t *testing.T) {
	// A request belonging to another mentor must not be reachable through a
	// different mentor's URL.
	repo := &adminRequestsMockRepo{request: &models.MentorClientRequest{ID: "req-1", MentorID: "mentor-2", Status: models.StatusPending}}
	svc := services.NewAdminRequestsService(repo, &capturingTracker{})

	_, err := svc.UpdateRequestStatus(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "req-1", models.StatusDone)
	if !errors.Is(err, services.ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
	if repo.updateCalled {
		t.Error("UpdateStatus must not be called for a foreign request")
	}
}

func TestAdminRequestsService_ModeratorRoleForbidden(t *testing.T) {
	repo := &adminRequestsMockRepo{request: requestOf(models.StatusPending), list: []*models.MentorClientRequest{requestOf(models.StatusPending)}}
	svc := services.NewAdminRequestsService(repo, &capturingTracker{})
	session := adminSession(models.ModeratorRoleModerator)
	ctx := context.Background()

	if _, err := svc.ListMentorRequests(ctx, session, "mentor-1", ""); !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Errorf("ListMentorRequests: expected ErrAdminForbiddenAction, got %v", err)
	}
	if _, err := svc.GetMentorRequest(ctx, session, "mentor-1", "req-1"); !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Errorf("GetMentorRequest: expected ErrAdminForbiddenAction, got %v", err)
	}
	if _, err := svc.UpdateRequestStatus(ctx, session, "mentor-1", "req-1", models.StatusDone); !errors.Is(err, services.ErrAdminForbiddenAction) {
		t.Errorf("UpdateRequestStatus: expected ErrAdminForbiddenAction, got %v", err)
	}
	if repo.updateCalled {
		t.Error("UpdateStatus must not be called for a moderator")
	}
}

func TestAdminRequestsService_ListMentorRequests_StatusFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   models.RequestStatus
		wantArgs []models.RequestStatus
		wantErr  bool
	}{
		{"no filter means all statuses", "", nil, false},
		{"single status filter", models.StatusDeclined, []models.RequestStatus{models.StatusDeclined}, false},
		{"unknown status is rejected", models.RequestStatus("nope"), nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &adminRequestsMockRepo{list: []*models.MentorClientRequest{requestOf(models.StatusDeclined)}}
			svc := services.NewAdminRequestsService(repo, &capturingTracker{})

			response, err := svc.ListMentorRequests(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ListMentorRequests() error = %v", err)
			}
			if response.Total != 1 || len(response.Requests) != 1 {
				t.Errorf("expected 1 request, got total=%d len=%d", response.Total, len(response.Requests))
			}
			if len(repo.listArgs) != len(tt.wantArgs) {
				t.Fatalf("expected statuses %v, got %v", tt.wantArgs, repo.listArgs)
			}
			for i, status := range tt.wantArgs {
				if repo.listArgs[i] != status {
					t.Errorf("expected status %q at %d, got %q", status, i, repo.listArgs[i])
				}
			}
		})
	}
}

func TestAdminRequestsService_GetMentorRequest_NotFound(t *testing.T) {
	repo := &adminRequestsMockRepo{getErr: errors.New("no rows in result set")}
	svc := services.NewAdminRequestsService(repo, &capturingTracker{})

	_, err := svc.GetMentorRequest(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1", "req-1")
	if !errors.Is(err, services.ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
}
