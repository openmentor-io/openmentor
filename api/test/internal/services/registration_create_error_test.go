package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/repository"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

func registrationRequest() *models.RegisterMentorRequest {
	return &models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Username:     "johndoe",
		CaptchaToken: "token123456789012345",
	}
}

// TestRegisterMentorReportsTakenUsername: the live availability check races the
// submit, so the unique-constraint violation is what the registrant actually
// hits — and it has to come back as an actionable field error, not the generic
// "something went wrong".
func TestRegisterMentorReportsTakenUsername(t *testing.T) {
	repo := &recordingRegistrationRepo{createErr: repository.ErrSlugTaken}
	svc := services.NewRegistrationService(repo, nil, &config.Config{}, captchaOKClient{}, &capturingTracker{})

	resp, err := svc.RegisterMentor(context.Background(), registrationRequest())

	if !errors.Is(err, repository.ErrSlugTaken) {
		t.Fatalf("RegisterMentor() error = %v, want it to wrap ErrSlugTaken", err)
	}
	if resp == nil || resp.Success {
		t.Fatalf("RegisterMentor() response = %+v, want an unsuccessful response", resp)
	}
	if resp.Reason != "username_taken" {
		t.Errorf("response reason = %q, want username_taken (the form maps it to the username field)", resp.Reason)
	}
}

// TestRegisterMentorKeepsDatabaseErrorCause: the cause must survive into the
// returned error, or the only record of why the INSERT failed is gone.
func TestRegisterMentorKeepsDatabaseErrorCause(t *testing.T) {
	dbErr := errors.New("connection refused")
	repo := &recordingRegistrationRepo{createErr: dbErr}
	svc := services.NewRegistrationService(repo, nil, &config.Config{}, captchaOKClient{}, &capturingTracker{})

	resp, err := svc.RegisterMentor(context.Background(), registrationRequest())

	if !errors.Is(err, dbErr) {
		t.Fatalf("RegisterMentor() error = %v, want it to wrap the repository error", err)
	}
	if resp == nil || resp.Reason != "" {
		t.Fatalf("RegisterMentor() response = %+v, want the generic failure with no field reason", resp)
	}
}
