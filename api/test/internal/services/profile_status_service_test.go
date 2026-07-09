package services_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/internal/services"
	apperrors "github.com/openmentor-io/openmentor-api/pkg/errors"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

func init() {
	// Initialize logger for tests (service logs on status changes)
	_ = logger.Initialize(logger.Config{
		Level:       "info",
		Environment: "test",
		ServiceName: "openmentor-api-test",
	})
}

// statusMockRepo implements services.ProfileMentorRepository for status toggle tests
type statusMockRepo struct {
	mentor              *models.Mentor
	getErr              error
	setStatusErr        error
	setStatusCalled     bool
	setStatusMentorID   string
	setStatusStatus     string
	refreshCacheCalled  bool
	refreshCacheErr     error
	touchUpdatedAtCalls int
}

func (m *statusMockRepo) GetByMentorId(ctx context.Context, mentorID string, opts models.FilterOptions) (*models.Mentor, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.mentor, nil
}

func (m *statusMockRepo) GetTagIDByName(ctx context.Context, tagName string) (string, error) {
	return "tag-id", nil
}

func (m *statusMockRepo) Update(ctx context.Context, mentorID string, updates map[string]interface{}) error {
	return nil
}

func (m *statusMockRepo) UpdateMentorTags(ctx context.Context, mentorID string, tagIDs []string) error {
	return nil
}

func (m *statusMockRepo) TouchUpdatedAt(ctx context.Context, mentorID string) error {
	m.touchUpdatedAtCalls++
	return nil
}

func (m *statusMockRepo) SetMentorStatus(ctx context.Context, mentorID, status string) error {
	m.setStatusCalled = true
	m.setStatusMentorID = mentorID
	m.setStatusStatus = status
	return m.setStatusErr
}

func (m *statusMockRepo) RefreshCache() error {
	m.refreshCacheCalled = true
	return m.refreshCacheErr
}

var _ services.ProfileMentorRepository = (*statusMockRepo)(nil)

// capturingTracker records analytics events for assertions
type capturingTracker struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	event      string
	distinctID string
	properties map[string]interface{}
}

func (t *capturingTracker) Track(ctx context.Context, event string, distinctID string, properties map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, capturedEvent{event: event, distinctID: distinctID, properties: properties})
}

func (t *capturingTracker) last() *capturedEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) == 0 {
		return nil
	}
	return &t.events[len(t.events)-1]
}

func newStatusTestService(repo *statusMockRepo, tracker *capturingTracker) *services.ProfileService {
	return services.NewProfileService(repo, nil, &config.Config{}, nil, tracker)
}

func TestProfileService_SetProfileStatusByMentorId_ActiveToInactive(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "active"}}
	tracker := &capturingTracker{}
	svc := newStatusTestService(repo, tracker)

	err := svc.SetProfileStatusByMentorId(context.Background(), "mentor-1", "inactive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !repo.setStatusCalled {
		t.Error("SetMentorStatus should have been called")
	}
	if repo.setStatusMentorID != "mentor-1" || repo.setStatusStatus != "inactive" {
		t.Errorf("SetMentorStatus called with (%s, %s), want (mentor-1, inactive)", repo.setStatusMentorID, repo.setStatusStatus)
	}
	if !repo.refreshCacheCalled {
		t.Error("RefreshCache should have been called after status change")
	}

	event := tracker.last()
	if event == nil {
		t.Fatal("expected analytics event to be tracked")
	}
	if event.event != "mentor_profile_status_changed" {
		t.Errorf("event = %q, want mentor_profile_status_changed", event.event)
	}
	if event.properties["outcome"] != "success" {
		t.Errorf("outcome = %v, want success", event.properties["outcome"])
	}
	if event.properties["from_status"] != "active" || event.properties["status"] != "inactive" {
		t.Errorf("from/to = %v/%v, want active/inactive", event.properties["from_status"], event.properties["status"])
	}
}

func TestProfileService_SetProfileStatusByMentorId_InactiveToActive(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "inactive"}}
	tracker := &capturingTracker{}
	svc := newStatusTestService(repo, tracker)

	err := svc.SetProfileStatusByMentorId(context.Background(), "mentor-1", "active")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.setStatusStatus != "active" {
		t.Errorf("SetMentorStatus status = %s, want active", repo.setStatusStatus)
	}
	if !repo.refreshCacheCalled {
		t.Error("RefreshCache should have been called after status change")
	}

	event := tracker.last()
	if event == nil || event.properties["outcome"] != "success" {
		t.Fatalf("expected success analytics event, got %+v", event)
	}
}

func TestProfileService_SetProfileStatusByMentorId_RejectsPendingAndDeclined(t *testing.T) {
	for _, currentStatus := range []string{"pending", "declined"} {
		t.Run(currentStatus, func(t *testing.T) {
			repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: currentStatus}}
			tracker := &capturingTracker{}
			svc := newStatusTestService(repo, tracker)

			err := svc.SetProfileStatusByMentorId(context.Background(), "mentor-1", "active")
			if !errors.Is(err, services.ErrProfileStatusNotToggleable) {
				t.Fatalf("error = %v, want ErrProfileStatusNotToggleable", err)
			}

			if repo.setStatusCalled {
				t.Error("SetMentorStatus should not have been called")
			}
			if repo.refreshCacheCalled {
				t.Error("RefreshCache should not have been called")
			}

			event := tracker.last()
			if event == nil || event.properties["outcome"] != "invalid_transition" {
				t.Fatalf("expected invalid_transition analytics event, got %+v", event)
			}
		})
	}
}

func TestProfileService_SetProfileStatusByMentorId_RejectsUnsupportedTargetStatus(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "active"}}
	tracker := &capturingTracker{}
	svc := newStatusTestService(repo, tracker)

	err := svc.SetProfileStatusByMentorId(context.Background(), "mentor-1", "declined")
	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if repo.setStatusCalled {
		t.Error("SetMentorStatus should not have been called")
	}
}

func TestProfileService_SetProfileStatusByMentorId_MentorNotFound(t *testing.T) {
	repo := &statusMockRepo{getErr: errors.New("no rows")}
	tracker := &capturingTracker{}
	svc := newStatusTestService(repo, tracker)

	err := svc.SetProfileStatusByMentorId(context.Background(), "missing-mentor", "inactive")
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}

	event := tracker.last()
	if event == nil || event.properties["outcome"] != "mentor_not_found" {
		t.Fatalf("expected mentor_not_found analytics event, got %+v", event)
	}
}

func TestProfileService_SetProfileStatusByMentorId_UpdateFails(t *testing.T) {
	repo := &statusMockRepo{
		mentor:       &models.Mentor{MentorID: "mentor-1", Status: "active"},
		setStatusErr: errors.New("db down"),
	}
	tracker := &capturingTracker{}
	svc := newStatusTestService(repo, tracker)

	err := svc.SetProfileStatusByMentorId(context.Background(), "mentor-1", "inactive")
	if err == nil {
		t.Fatal("expected error when SetMentorStatus fails")
	}
	if repo.refreshCacheCalled {
		t.Error("RefreshCache should not have been called on update failure")
	}

	event := tracker.last()
	if event == nil || event.properties["outcome"] != "update_failed" {
		t.Fatalf("expected update_failed analytics event, got %+v", event)
	}
}
