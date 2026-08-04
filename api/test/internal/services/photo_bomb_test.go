package services_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"github.com/openmentor-io/openmentor/api/test/imagefixture"
)

// captchaOKClient is an httpclient.Client whose Turnstile siteverify response
// always succeeds, so registration tests reach the code under test.
type captchaOKClient struct{}

func (captchaOKClient) Post(_, _ string, _ io.Reader) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
	}, nil
}

func (captchaOKClient) Get(_ string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (captchaOKClient) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

// bombBase64 is the wire form of an upload: base64, as the JSON endpoints take
// it. 10000x10000 is ~12 KiB encoded and ~95 MiB decoded.
func bombBase64(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(imagefixture.BombPNG(t, 10000, 10000))
}

func photoDataURI(t *testing.T) string {
	t.Helper()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(imagefixture.PhotoPNG(t, 2000, 2000))
}

// unreachableStorage is a real client pointed at a dead endpoint: uploads fail
// with a connection error instead of panicking on a zero-value S3 client, which
// lets the happy path run to completion without touching a network service.
func unreachableStorage(t *testing.T) *s3storage.StorageClient {
	t.Helper()
	client, err := s3storage.NewStorageClient("key", "secret", "bucket", "http://127.0.0.1:1", "auto")
	if err != nil {
		t.Fatalf("NewStorageClient() error = %v", err)
	}
	return client
}

// TestRegistrationRejectsDecompressionBomb: registration classified the photo —
// i.e. fully decoded it — while building the INSERT, with no pixel bound at all.
// The bomb must now be refused before any DB write.
func TestRegistrationRejectsDecompressionBomb(t *testing.T) {
	repo := &recordingRegistrationRepo{}
	svc := services.NewRegistrationService(repo, &s3storage.StorageClient{}, &config.Config{}, captchaOKClient{}, &capturingTracker{})

	req := &models.RegisterMentorRequest{Name: "John Doe", Email: "john@example.com"}
	req.ProfilePicture.Image = bombBase64(t)
	req.ProfilePicture.ContentType = "image/png"

	resp, err := svc.RegisterMentor(context.Background(), req)

	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("RegisterMentor() error = %v, want an invalid-input error", err)
	}
	if resp == nil || resp.Reason != "invalid_photo" {
		t.Fatalf("RegisterMentor() response = %+v, want reason invalid_photo", resp)
	}
	if repo.createCalls != 0 {
		t.Errorf("no mentor row may be created for a rejected photo, got %d CreateMentor calls", repo.createCalls)
	}
}

func TestRegistrationAcceptsOrdinaryPhoto(t *testing.T) {
	repo := &recordingRegistrationRepo{}
	svc := services.NewRegistrationService(repo, unreachableStorage(t), &config.Config{}, captchaOKClient{}, &capturingTracker{})

	req := &models.RegisterMentorRequest{Name: "John Doe", Email: "john@example.com"}
	req.ProfilePicture.Image = photoDataURI(t)
	req.ProfilePicture.ContentType = "image/png"

	resp, err := svc.RegisterMentor(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterMentor() error = %v, want nil", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("RegisterMentor() response = %+v, want success", resp)
	}
	if repo.createCalls != 1 {
		t.Fatalf("CreateMentor calls = %d, want 1", repo.createCalls)
	}
	// The style is classified once, from the same decode that validated the
	// image; a near-white 2000x2000 photo is a 'hero'.
	if got := repo.createdFields["photo_style"]; got != "hero" {
		t.Errorf("photo_style = %v, want hero", got)
	}
}

func TestProfilePictureUploadRejectsDecompressionBomb(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "active"}}
	svc := services.NewProfileService(repo, &s3storage.StorageClient{}, &config.Config{}, nil, &capturingTracker{})

	url, err := svc.UploadPictureByMentorId(context.Background(), "mentor-1", &models.UploadProfilePictureRequest{
		Image:       bombBase64(t),
		ContentType: "image/png",
	})

	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("UploadPictureByMentorId() error = %v, want an invalid-input error", err)
	}
	if url != "" {
		t.Errorf("UploadPictureByMentorId() url = %q, want empty", url)
	}
	// Nothing was stored and nothing was written: this endpoint used to upload
	// three objects before it ever looked at the pixels.
	if repo.touchUpdatedAtCalls != 0 {
		t.Errorf("no DB write may happen, got %d TouchUpdatedAt calls", repo.touchUpdatedAtCalls)
	}
}

// TestProfilePictureUploadRejectsSixteenBitBomb: 4000x4000 is inside the pixel
// bound and affordable as an 8-bit PNG (64 MiB), but as 16-bit RGBA the same
// geometry decodes into 128 MiB. The upload path — not just the classifier — has
// to refuse it, since this is where the uploader's 400 comes from.
func TestProfilePictureUploadRejectsSixteenBitBomb(t *testing.T) {
	repo := &statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "active"}}
	svc := services.NewProfileService(repo, &s3storage.StorageClient{}, &config.Config{}, nil, &capturingTracker{})

	url, err := svc.UploadPictureByMentorId(context.Background(), "mentor-1", &models.UploadProfilePictureRequest{
		Image:       base64.StdEncoding.EncodeToString(imagefixture.RGBA64BombPNG(t, 4000, 4000)),
		ContentType: "image/png",
	})

	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("UploadPictureByMentorId() error = %v, want an invalid-input error", err)
	}
	if url != "" {
		t.Errorf("UploadPictureByMentorId() url = %q, want empty", url)
	}
	if repo.touchUpdatedAtCalls != 0 {
		t.Errorf("no DB write may happen, got %d TouchUpdatedAt calls", repo.touchUpdatedAtCalls)
	}
}

// TestAdminPictureUploadRejectsDecompressionBomb covers the third photo path:
// the admin endpoint delegates to ProfileService, so it must inherit the bound.
func TestAdminPictureUploadRejectsDecompressionBomb(t *testing.T) {
	profileSvc := services.NewProfileService(
		&statusMockRepo{mentor: &models.Mentor{MentorID: "mentor-1", Status: "pending"}},
		&s3storage.StorageClient{}, &config.Config{}, nil, &capturingTracker{},
	)
	adminRepo := &adminMockRepo{mentor: pendingMentorDetails()}
	svc := services.NewAdminMentorsService(adminRepo, profileSvc, &config.Config{}, nil, &capturingTracker{})

	url, err := svc.UploadMentorPicture(context.Background(), adminSession(models.ModeratorRoleAdmin), "mentor-1",
		&models.UploadProfilePictureRequest{Image: bombBase64(t), ContentType: "image/png"})

	if !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatalf("UploadMentorPicture() error = %v, want an invalid-input error", err)
	}
	if url != "" {
		t.Errorf("UploadMentorPicture() url = %q, want empty", url)
	}
}
