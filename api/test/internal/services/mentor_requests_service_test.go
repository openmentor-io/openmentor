package services_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
)

// The mentor's own request inbox (C3's second half). The single-winner property
// belongs to SQL and is tested against a real database
// (api/internal/repository/contact_gate_db_test.go). What matters here is what the
// LOSER does: it must report the conflict and, above all, must not send the mentee
// an email about a transition that did not happen.

// mentorRequestsRepoFake is the client-request surface the inbox uses.
type mentorRequestsRepoFake struct {
	request *models.MentorClientRequest
	// notApplied makes the compare-and-set write match no row, the way a
	// concurrent transition does.
	notApplied     bool
	statusCalls    int
	declineCalls   int
	lastExpected   models.RequestStatus
	lastNewStatus  models.RequestStatus
	lastDeclineFor models.RequestStatus
}

func (f *mentorRequestsRepoFake) GetByMentor(context.Context, string, []models.RequestStatus) ([]*models.MentorClientRequest, error) {
	return []*models.MentorClientRequest{f.request}, nil
}

func (f *mentorRequestsRepoFake) GetByID(context.Context, string) (*models.MentorClientRequest, error) {
	if f.request == nil {
		return nil, errors.New("not found")
	}
	return f.request, nil
}

func (f *mentorRequestsRepoFake) UpdateStatus(
	_ context.Context,
	_ string,
	expected, status models.RequestStatus,
) (bool, error) {

	f.statusCalls++
	f.lastExpected = expected
	f.lastNewStatus = status
	if f.notApplied {
		return false, nil
	}
	f.request.Status = status
	return true, nil
}

func (f *mentorRequestsRepoFake) UpdateDecline(
	_ context.Context,
	_ string,
	expected models.RequestStatus,
	_ models.DeclineReason,
	_ string,
) (bool, error) {

	f.declineCalls++
	f.lastDeclineFor = expected
	if f.notApplied {
		return false, nil
	}
	f.request.Status = models.StatusDeclined
	return true, nil
}

var _ services.MentorRequestsRepository = (*mentorRequestsRepoFake)(nil)

// countingTriggerClient answers every request with a bare 200 and counts them, so
// a test can assert that no worker trigger — and therefore no mentee email — was
// fired.
type countingTriggerClient struct {
	calls atomic.Int32
	fired chan struct{}
}

func newCountingTriggerClient() *countingTriggerClient {
	return &countingTriggerClient{fired: make(chan struct{}, 8)}
}

func (c *countingTriggerClient) Do(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	select {
	case c.fired <- struct{}{}:
	default:
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func (c *countingTriggerClient) Post(string, string, io.Reader) (*http.Response, error) {
	panic("httpclient.Client.Post carries no context; use Do")
}

func (c *countingTriggerClient) Get(string) (*http.Response, error) {
	return c.Do(nil) //nolint:bodyclose // fake response, no real body
}

var _ httpclient.Client = (*countingTriggerClient)(nil)

func requestsTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.EventTriggers.RequestProcessFinishedTriggerURL = "https://worker.example/jobs/request-finished?id="
	return cfg
}

// TestUpdateStatusThatDidNotApplyNotifiesNobody: a blind UPDATE always "succeeded",
// so a mentor completing a request the mentee had already withdrawn still triggered
// the finished-session email. The write's own verdict now gates the trigger.
func TestUpdateStatusThatDidNotApplyNotifiesNobody(t *testing.T) {
	repo := &mentorRequestsRepoFake{
		request:    &models.MentorClientRequest{ID: "req-1", MentorID: "mentor-1", Status: models.StatusWorking},
		notApplied: true,
	}
	client := newCountingTriggerClient()
	svc := services.NewMentorRequestsService(repo, requestsTestConfig(), client, &capturingTracker{})

	got, err := svc.UpdateStatus(context.Background(), "mentor-1", "req-1", models.StatusDone)
	if !errors.Is(err, services.ErrInvalidStatusTransition) {
		t.Fatalf("UpdateStatus() error = %v, want ErrInvalidStatusTransition", err)
	}
	if got != nil {
		t.Errorf("UpdateStatus() request = %+v, want nil", got)
	}
	if repo.lastExpected != models.StatusWorking {
		t.Errorf("expected status passed to the write = %q, want working", repo.lastExpected)
	}

	// The trigger is fire-and-forget; give a goroutine that must not exist a
	// chance to prove it does.
	select {
	case <-client.fired:
		t.Fatal("a transition that did not apply must not fire the mentee notification")
	case <-time.After(150 * time.Millisecond):
	}
}

// TestUpdateStatusThatAppliedNotifies is the other half: the guard above must not
// have silenced the real notification.
func TestUpdateStatusThatAppliedNotifies(t *testing.T) {
	repo := &mentorRequestsRepoFake{
		request: &models.MentorClientRequest{ID: "req-1", MentorID: "mentor-1", Status: models.StatusWorking},
	}
	client := newCountingTriggerClient()
	svc := services.NewMentorRequestsService(repo, requestsTestConfig(), client, &capturingTracker{})

	if _, err := svc.UpdateStatus(context.Background(), "mentor-1", "req-1", models.StatusDone); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	select {
	case <-client.fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a completed request must still notify the mentee")
	}
}

// TestDeclineThatDidNotApplyIsReported: same for the decline, which carries a
// reason and a comment — writing them onto a request somebody else already
// finished would rewrite history AND mail the mentee about it.
func TestDeclineThatDidNotApplyIsReported(t *testing.T) {
	repo := &mentorRequestsRepoFake{
		request:    &models.MentorClientRequest{ID: "req-1", MentorID: "mentor-1", Status: models.StatusPending},
		notApplied: true,
	}
	client := newCountingTriggerClient()
	svc := services.NewMentorRequestsService(repo, requestsTestConfig(), client, &capturingTracker{})

	got, err := svc.DeclineRequest(context.Background(), "mentor-1", "req-1", &models.DeclineRequestPayload{
		Reason:  models.DeclineTopicMismatch,
		Comment: "not my field",
	})
	if !errors.Is(err, services.ErrCannotDeclineRequest) {
		t.Fatalf("DeclineRequest() error = %v, want ErrCannotDeclineRequest", err)
	}
	if got != nil {
		t.Errorf("DeclineRequest() request = %+v, want nil", got)
	}
	if repo.lastDeclineFor != models.StatusPending {
		t.Errorf("expected status passed to the decline = %q, want pending", repo.lastDeclineFor)
	}

	select {
	case <-client.fired:
		t.Fatal("a decline that did not apply must not fire the mentee notification")
	case <-time.After(150 * time.Millisecond):
	}
}
