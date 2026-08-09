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

// The contact submission service (C3). Whether a non-active mentor can be
// contacted is decided in SQL and tested against a real database
// (api/internal/repository/contact_gate_db_test.go); what these tests pin is the
// service's half — a typed, machine-readable rejection, and no request id or
// calendar link handed back for a request that was never created.

// contactRepoFake stands in for the client-request repository at the one call
// ContactService makes.
type contactRepoFake struct {
	created *repository.CreatedClientRequest
	err     error
	calls   int
}

func (f *contactRepoFake) Create(_ context.Context, _ *models.ClientRequest) (*repository.CreatedClientRequest, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.created, nil
}

var _ services.ContactClientRequestRepository = (*contactRepoFake)(nil)

func contactRequestFixture() *models.ContactMentorRequest {
	return &models.ContactMentorRequest{
		Name:         "Mentee",
		Email:        "mentee@example.test",
		Experience:   "Junior",
		MentorID:     "4821fee2-7601-41ad-8798-70d57f0b2acc",
		Intro:        "I would like an hour of your time",
		CaptchaToken: "token",
	}
}

// TestContactRejectionIsTypedAndCarriesNothing: the mentor lookup used to happen
// AFTER the insert, so an ineligible mentor still produced a request row and a
// "success" response. The refusal now comes from the write itself and must reach
// the client as a reason it can act on, with no request id.
func TestContactRejectionIsTypedAndCarriesNothing(t *testing.T) {
	repo := &contactRepoFake{err: repository.ErrMentorNotContactable}
	svc := services.NewContactService(repo, &config.Config{}, captchaOK, &capturingTracker{})

	resp, err := svc.SubmitContactForm(context.Background(), contactRequestFixture())

	if !errors.Is(err, services.ErrMentorNotContactable) {
		t.Fatalf("SubmitContactForm() error = %v, want ErrMentorNotContactable", err)
	}
	if resp == nil || resp.Success {
		t.Fatalf("response = %+v, want an unsuccessful response", resp)
	}
	if resp.Reason != "mentor_not_contactable" {
		t.Errorf("reason = %q, want mentor_not_contactable", resp.Reason)
	}
	if resp.RequestID != "" {
		t.Errorf("requestId = %q, want empty: no request was created", resp.RequestID)
	}
	if resp.CalendarURL != "" {
		t.Errorf("calendar_url = %q, want empty: an ineligible mentor's link is not disclosed", resp.CalendarURL)
	}
}

// TestContactSuccessReturnsWhatTheWriteReturned: the calendar link comes from the
// gated INSERT, not from a second read that could see a different mentor state.
func TestContactSuccessReturnsWhatTheWriteReturned(t *testing.T) {
	repo := &contactRepoFake{created: &repository.CreatedClientRequest{
		ID:          "request-1",
		CalendarURL: "https://cal.example/mentor",
	}}
	tracker := &capturingTracker{}
	svc := services.NewContactService(repo, &config.Config{}, captchaOK, tracker)

	resp, err := svc.SubmitContactForm(context.Background(), contactRequestFixture())
	if err != nil {
		t.Fatalf("SubmitContactForm() error = %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("response = %+v, want success", resp)
	}
	if resp.RequestID != "request-1" || resp.CalendarURL != "https://cal.example/mentor" {
		t.Errorf("response = %+v, want the id and link the write returned", resp)
	}
	if last := tracker.last(); last == nil || last.properties["calendar_url_available"] != true {
		t.Errorf("tracked properties = %v, want calendar_url_available=true", last)
	}
}

// TestContactDoesNotWriteWhenTheCaptchaFails stays here because the ordering is a
// security property: the write is the only thing that touches the mentor, and it
// must not be reachable without a captcha.
func TestContactDoesNotWriteWhenTheCaptchaFails(t *testing.T) {
	repo := &contactRepoFake{}
	svc := services.NewContactService(repo, &config.Config{}, captchaFail, &capturingTracker{})

	resp, err := svc.SubmitContactForm(context.Background(), contactRequestFixture())
	if err == nil {
		t.Fatal("SubmitContactForm() = nil error, want a captcha rejection")
	}
	if resp == nil || resp.Success {
		t.Fatalf("response = %+v, want an unsuccessful response", resp)
	}
	if repo.calls != 0 {
		t.Errorf("Create calls = %d, want 0", repo.calls)
	}
}
