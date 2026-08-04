package services_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
	"github.com/openmentor-io/openmentor/api/test/imagefixture"
)

// captchaFailClient is an httpclient.Client whose Turnstile siteverify response
// always rejects the token.
type captchaFailClient struct{}

func (captchaFailClient) Post(_, _ string, _ io.Reader) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"success":false,"error-codes":["invalid-input-response"]}`)),
	}, nil
}

func (captchaFailClient) Get(_ string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

func (captchaFailClient) Do(_ *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

// TestRegistrationVerifiesCaptchaBeforeTouchingThePhoto pins the order of the
// two guards. /register-mentor is unauthenticated, and the image decode is by
// far the most expensive thing it does (tens of megabytes for a payload of a
// few kilobytes), so an attacker must have to solve a Turnstile challenge
// before any of that work is reachable.
func TestRegistrationVerifiesCaptchaBeforeTouchingThePhoto(t *testing.T) {
	repo := &recordingRegistrationRepo{}
	svc := services.NewRegistrationService(repo, &s3storage.StorageClient{}, &config.Config{}, captchaFailClient{}, &capturingTracker{})

	req := &models.RegisterMentorRequest{Name: "John Doe", Email: "john@example.com"}
	// A legitimate 2000x2000 photo: it passes every validator, so nothing but
	// the guard order can keep it from being decoded (~16 MiB of pixels).
	req.ProfilePicture.Image = base64.StdEncoding.EncodeToString(imagefixture.PhotoPNG(t, 2000, 2000))
	req.ProfilePicture.ContentType = "image/png"

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	resp, err := svc.RegisterMentor(context.Background(), req)

	runtime.ReadMemStats(&after)

	if err == nil || !strings.Contains(err.Error(), "captcha") {
		t.Fatalf("RegisterMentor() error = %v, want a captcha failure", err)
	}
	if resp == nil || resp.Success {
		t.Fatalf("RegisterMentor() response = %+v, want an unsuccessful response", resp)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
		t.Errorf("a rejected captcha allocated %d bytes: the photo was decoded first", allocated)
	}
}

// TestRegistrationRejectsBadCaptchaBeforeBadPhoto: with both a bogus captcha
// and an unusable photo, the captcha is what the caller is told about.
func TestRegistrationRejectsBadCaptchaBeforeBadPhoto(t *testing.T) {
	repo := &recordingRegistrationRepo{}
	svc := services.NewRegistrationService(repo, &s3storage.StorageClient{}, &config.Config{}, captchaFailClient{}, &capturingTracker{})

	req := &models.RegisterMentorRequest{Name: "John Doe", Email: "john@example.com"}
	req.ProfilePicture.Image = base64.StdEncoding.EncodeToString([]byte("not an image at all"))
	req.ProfilePicture.ContentType = "image/png"

	resp, _ := svc.RegisterMentor(context.Background(), req)

	if resp == nil || resp.Reason != "" {
		t.Fatalf("RegisterMentor() response = %+v, want the captcha rejection, not invalid_photo", resp)
	}
}
