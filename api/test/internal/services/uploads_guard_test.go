package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

func init() {
	// The photo paths count their outcome; the metric vectors are nil until
	// Init runs, so an increment would nil-deref in tests.
	metrics.Init("openmentor-api-test")
}

// recordingRegistrationRepo records every write RegisterMentor performs, so a
// test can assert that a rejected registration wrote NOTHING.
type recordingRegistrationRepo struct {
	createCalls   int
	tagCalls      int
	createdFields map[string]interface{}
}

func (r *recordingRegistrationRepo) GetTagIDByName(_ context.Context, _ string) (string, error) {
	return "tag-id", nil
}

func (r *recordingRegistrationRepo) CreateMentor(_ context.Context, fields map[string]interface{}) (string, int, string, error) {
	r.createCalls++
	r.createdFields = fields
	return "mentor-uuid", 42, "john-doe-42", nil
}

func (r *recordingRegistrationRepo) UpdateMentorTags(_ context.Context, _ string, _ []string) error {
	r.tagCalls++
	return nil
}

var _ services.RegistrationMentorRepository = (*recordingRegistrationRepo)(nil)

// TestRegistrationWithPhotoRejectedWhenUploadsUnconfigured is the P1 crash in
// test form: the nil storage client used to be injected anyway and only blow
// up in the detached upload goroutine — AFTER the mentor row had committed and
// the registrant had been told they succeeded.
func TestRegistrationWithPhotoRejectedWhenUploadsUnconfigured(t *testing.T) {
	repo := &recordingRegistrationRepo{}
	svc := services.NewRegistrationService(repo, nil, &config.Config{}, nil, &capturingTracker{})

	req := &models.RegisterMentorRequest{
		Name:  "John Doe",
		Email: "john@example.com",
	}
	req.ProfilePicture.Image = "aGVsbG8="
	req.ProfilePicture.ContentType = "image/png"

	resp, err := svc.RegisterMentor(context.Background(), req)

	if !errors.Is(err, services.ErrUploadsUnavailable) {
		t.Fatalf("RegisterMentor() error = %v, want ErrUploadsUnavailable", err)
	}
	if resp == nil || resp.Success {
		t.Fatalf("RegisterMentor() response = %+v, want an unsuccessful response", resp)
	}
	if resp.Reason != "uploads_unavailable" {
		t.Errorf("response reason = %q, want uploads_unavailable", resp.Reason)
	}
	if repo.createCalls != 0 || repo.tagCalls != 0 {
		t.Errorf("no DB write may happen: createCalls=%d tagCalls=%d", repo.createCalls, repo.tagCalls)
	}
}

func TestProfilePictureUploadRejectedWhenUploadsUnconfigured(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "active"}}
	svc := services.NewProfileService(repo, nil, &config.Config{}, nil, &capturingTracker{})

	url, err := svc.UploadPictureByMentorId(context.Background(), "mentor-1", &models.UploadProfilePictureRequest{
		Image:       "aGVsbG8=",
		ContentType: "image/png",
	})

	if !errors.Is(err, services.ErrUploadsUnavailable) {
		t.Fatalf("UploadPictureByMentorId() error = %v, want ErrUploadsUnavailable", err)
	}
	if url != "" {
		t.Errorf("UploadPictureByMentorId() url = %q, want empty", url)
	}
	if repo.touchUpdatedAtCalls != 0 {
		t.Errorf("no DB write may happen, got %d TouchUpdatedAt calls", repo.touchUpdatedAtCalls)
	}
}
