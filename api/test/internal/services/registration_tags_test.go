package services_test

import (
	"context"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// Registration tag resolution (C2). The service used to re-implement the tag loop
// non-strictly: an unknown name was warned about and dropped, and the registration
// succeeded without it. That is exactly what happened in production — the frontend
// offered "Security", the tag had never been seeded, and every mentor who picked it
// was created without it (see migration 000009).

func registrationFixture(tags []string) *models.RegisterMentorRequest {
	return &models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Job:          "Staff Engineer",
		Workplace:    "OpenMentor",
		Experience:   "5-10",
		Price:        "Free",
		Tags:         tags,
		About:        "about text",
		Description:  "short description",
		Competencies: "competencies text",
		CaptchaToken: "token",
	}
}

// TestRegistrationRefusesUnknownTags: no mentor row may be created at all — a
// registration is not retryable once the row exists (the email is unique, the
// username is claimed), so "created but wrong" cannot be corrected by resubmitting.
func TestRegistrationRefusesUnknownTags(t *testing.T) {
	repo := &recordingRegistrationRepo{unknownTags: map[string]bool{"Security": true}}
	svc := services.NewRegistrationService(repo, nil, &config.Config{}, captchaOK, &capturingTracker{})

	resp, err := svc.RegisterMentor(context.Background(),
		registrationFixture([]string{"Backend", "Security"}))

	if err == nil {
		t.Fatal("RegisterMentor() = nil error, want a rejection")
	}
	if resp == nil || resp.Success {
		t.Fatalf("RegisterMentor() response = %+v, want an unsuccessful response", resp)
	}
	if resp.Reason != "tags_invalid" {
		t.Errorf("response reason = %q, want tags_invalid", resp.Reason)
	}
	if repo.createCalls != 0 {
		t.Errorf("CreateMentor calls = %d, want 0: a rejected registration writes nothing", repo.createCalls)
	}
}

// TestRegistrationPassesResolvedTagsToTheInsert: the tags travel WITH the row, in
// one transaction, rather than in a follow-up call whose failure used to be logged
// and ignored.
func TestRegistrationPassesResolvedTagsToTheInsert(t *testing.T) {
	repo := &recordingRegistrationRepo{}
	svc := services.NewRegistrationService(repo, nil, &config.Config{}, captchaOK, &capturingTracker{})

	resp, err := svc.RegisterMentor(context.Background(),
		registrationFixture([]string{"Backend", "Frontend"}))
	if err != nil {
		t.Fatalf("RegisterMentor() error = %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("RegisterMentor() response = %+v, want success", resp)
	}
	if repo.createCalls != 1 {
		t.Fatalf("CreateMentor calls = %d, want 1", repo.createCalls)
	}

	want := []string{"tag-id-Backend", "tag-id-Frontend"}
	if len(repo.createdTagIDs) != len(want) {
		t.Fatalf("tag ids = %v, want %v", repo.createdTagIDs, want)
	}
	for i, id := range want {
		if repo.createdTagIDs[i] != id {
			t.Errorf("tag id %d = %q, want %q", i, repo.createdTagIDs[i], id)
		}
	}
}
