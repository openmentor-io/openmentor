package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
)

func TestProfileService_SubmitProfileByMentorId_FromDraft(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "draft"}}
	tracker := &capturingTracker{}
	svc := newStatusTestService(repo, tracker)

	err := svc.SubmitProfileByMentorId(context.Background(), "mentor-1")
	if err != nil {
		t.Fatalf("SubmitProfileByMentorId() error = %v", err)
	}

	if !repo.setStatusCalled || repo.setStatusStatus != "pending" {
		t.Errorf("expected status write to pending, got called=%v status=%q", repo.setStatusCalled, repo.setStatusStatus)
	}

	event := tracker.last()
	if event == nil || event.event != "mentor_profile_resubmitted" {
		t.Fatalf("expected mentor_profile_resubmitted event, got %+v", event)
	}
	if event.properties["outcome"] != "success" {
		t.Errorf("expected success outcome, got %v", event.properties["outcome"])
	}
}

func TestProfileService_SubmitProfileByMentorId_OnlyFromDraft(t *testing.T) {
	for _, status := range []string{"pending", "active", "inactive", "declined"} {
		t.Run(status, func(t *testing.T) {
			repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: status}}
			svc := newStatusTestService(repo, &capturingTracker{})

			err := svc.SubmitProfileByMentorId(context.Background(), "mentor-1")
			if !errors.Is(err, services.ErrProfileNotSubmittable) {
				t.Fatalf("expected ErrProfileNotSubmittable for status %s, got %v", status, err)
			}
			if repo.setStatusCalled {
				t.Error("no status write expected")
			}
		})
	}
}

func TestProfileService_SubmitProfileByMentorId_MentorNotFound(t *testing.T) {
	repo := &statusMockRepo{getErr: errors.New("not found")}
	svc := newStatusTestService(repo, &capturingTracker{})

	err := svc.SubmitProfileByMentorId(context.Background(), "missing")
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("expected not-found error, got %v", err)
	}
}
