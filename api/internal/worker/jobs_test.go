package worker

// Shared fakes and helpers for the job handler tests
// (job_*_test.go). The real dependencies are swapped for in-memory fakes:
// JobsRepository, EmailSender and analytics.Tracker.

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/email/templates"
)

// fakeRepo is an in-memory JobsRepository with error injection.
type fakeRepo struct {
	mentors            map[string]*JobMentor
	requests           map[string]*JobRequest
	requestsWithMentor map[string]*JobRequest
	moderators         map[string]*JobModerator
	reviews            map[string]*JobReview

	duplicates int

	mentorErr     error
	duplicatesErr error
	finalizeErr   error
	releaseErr    error
	// finalizeNotApplied makes FinalizeNewMentor report that no row matched,
	// which is what the real UPDATE's WHERE status = 'draft' does when the
	// registration leaves draft between the read and the write.
	finalizeNotApplied bool
	setStatusErr       error
	requestErr         error
	setRequestErr      error
	moderatorErr       error
	reviewErr          error

	finalized      []FinalizeNewMentorParams
	released       []FinalizeNewMentorParams
	statusUpdates  map[string]string // mentorID -> status
	requestUpdates map[string]string // requestID -> contact

	// Cron job fixtures (stage 3).
	stalePendingMentors   []JobMentor
	stalePendingRequests  map[string][]JobReminderRequest // mentorID -> requests
	staleProgressMentors  []JobMentor
	staleProgressRequests map[string][]JobReminderRequest // mentorID -> requests
	mentorsToDeactivate   []JobMentor
	stuckDraftMentors     []JobMentor
	activeMentorIDs       []string
	listStaleMentorsErr   error
	listStaleRequestsErr  error
	listDeactivateErr     error
	listStuckDraftErr     error
	deactivateErr         error
	listActiveErr         error
	setSortOrdersErr      error
	deactivated           []string
	sortOrderTransactions [][]SortOrderUpdate
	staleRequestsQueryLog []string // mentorIDs queried for reminder requests
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		mentors:               map[string]*JobMentor{},
		requests:              map[string]*JobRequest{},
		requestsWithMentor:    map[string]*JobRequest{},
		moderators:            map[string]*JobModerator{},
		reviews:               map[string]*JobReview{},
		statusUpdates:         map[string]string{},
		requestUpdates:        map[string]string{},
		stalePendingRequests:  map[string][]JobReminderRequest{},
		staleProgressRequests: map[string][]JobReminderRequest{},
	}
}

func (f *fakeRepo) GetJobMentorByID(_ context.Context, mentorID string) (*JobMentor, error) {
	if f.mentorErr != nil {
		return nil, f.mentorErr
	}
	if m, ok := f.mentors[mentorID]; ok {
		copied := *m
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeRepo) CountActiveMentorsByEmail(_ context.Context, _ string) (int, error) {
	if f.duplicatesErr != nil {
		return 0, f.duplicatesErr
	}
	return f.duplicates, nil
}

func (f *fakeRepo) FinalizeNewMentor(_ context.Context, params FinalizeNewMentorParams) (bool, error) {
	if f.finalizeErr != nil {
		return false, f.finalizeErr
	}
	if f.finalizeNotApplied {
		return false, nil
	}
	f.finalized = append(f.finalized, params)
	return true, nil
}

func (f *fakeRepo) ReleaseNewMentorFinalization(_ context.Context, params FinalizeNewMentorParams) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.released = append(f.released, params)
	return nil
}

func (f *fakeRepo) SetMentorStatus(_ context.Context, mentorID, status string) error {
	if f.setStatusErr != nil {
		return f.setStatusErr
	}
	f.statusUpdates[mentorID] = status
	return nil
}

func (f *fakeRepo) GetJobRequestByID(_ context.Context, requestID string) (*JobRequest, error) {
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	if r, ok := f.requests[requestID]; ok {
		copied := *r
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeRepo) GetJobRequestWithMentorName(_ context.Context, requestID string) (*JobRequest, error) {
	if f.requestErr != nil {
		return nil, f.requestErr
	}
	if r, ok := f.requestsWithMentor[requestID]; ok {
		copied := *r
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeRepo) SetRequestContactPending(_ context.Context, requestID, contact string) error {
	if f.setRequestErr != nil {
		return f.setRequestErr
	}
	f.requestUpdates[requestID] = contact
	return nil
}

func (f *fakeRepo) GetJobModeratorByID(_ context.Context, moderatorID string) (*JobModerator, error) {
	if f.moderatorErr != nil {
		return nil, f.moderatorErr
	}
	if m, ok := f.moderators[moderatorID]; ok {
		copied := *m
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeRepo) GetJobReviewByID(_ context.Context, reviewID string) (*JobReview, error) {
	if f.reviewErr != nil {
		return nil, f.reviewErr
	}
	if r, ok := f.reviews[reviewID]; ok {
		copied := *r
		return &copied, nil
	}
	return nil, nil
}

func (f *fakeRepo) ListMentorsWithStalePendingRequests(_ context.Context) ([]JobMentor, error) {
	if f.listStaleMentorsErr != nil {
		return nil, f.listStaleMentorsErr
	}
	return append([]JobMentor(nil), f.stalePendingMentors...), nil
}

func (f *fakeRepo) ListStalePendingRequests(_ context.Context, mentorID string) ([]JobReminderRequest, error) {
	if f.listStaleRequestsErr != nil {
		return nil, f.listStaleRequestsErr
	}
	f.staleRequestsQueryLog = append(f.staleRequestsQueryLog, mentorID)
	return append([]JobReminderRequest(nil), f.stalePendingRequests[mentorID]...), nil
}

func (f *fakeRepo) ListMentorsWithStaleInProgressRequests(_ context.Context) ([]JobMentor, error) {
	if f.listStaleMentorsErr != nil {
		return nil, f.listStaleMentorsErr
	}
	return append([]JobMentor(nil), f.staleProgressMentors...), nil
}

func (f *fakeRepo) ListStaleInProgressRequests(_ context.Context, mentorID string) ([]JobReminderRequest, error) {
	if f.listStaleRequestsErr != nil {
		return nil, f.listStaleRequestsErr
	}
	f.staleRequestsQueryLog = append(f.staleRequestsQueryLog, mentorID)
	return append([]JobReminderRequest(nil), f.staleProgressRequests[mentorID]...), nil
}

func (f *fakeRepo) ListMentorsToDeactivate(_ context.Context) ([]JobMentor, error) {
	if f.listDeactivateErr != nil {
		return nil, f.listDeactivateErr
	}
	return append([]JobMentor(nil), f.mentorsToDeactivate...), nil
}

func (f *fakeRepo) ListStuckDraftRegistrations(_ context.Context) ([]JobMentor, error) {
	if f.listStuckDraftErr != nil {
		return nil, f.listStuckDraftErr
	}
	return append([]JobMentor(nil), f.stuckDraftMentors...), nil
}

func (f *fakeRepo) DeactivateMentor(_ context.Context, mentorID string) error {
	if f.deactivateErr != nil {
		return f.deactivateErr
	}
	f.deactivated = append(f.deactivated, mentorID)
	return nil
}

func (f *fakeRepo) ListActiveMentorIDs(_ context.Context) ([]string, error) {
	if f.listActiveErr != nil {
		return nil, f.listActiveErr
	}
	return append([]string(nil), f.activeMentorIDs...), nil
}

func (f *fakeRepo) SetSortOrders(_ context.Context, updates []SortOrderUpdate) error {
	if f.setSortOrdersErr != nil {
		return f.setSortOrdersErr
	}
	f.sortOrderTransactions = append(f.sortOrderTransactions, append([]SortOrderUpdate(nil), updates...))
	return nil
}

// fakeEmailSender records every send attempt (including failed ones, to
// verify one failure does not skip the remaining sends).
type fakeEmailSender struct {
	attempts       []email.Message
	failTemplates  map[string]bool
	failRecipients map[string]bool
	failAll        bool
}

func (f *fakeEmailSender) Send(_ context.Context, msg email.Message) (string, error) {
	f.attempts = append(f.attempts, msg)
	if f.failAll || f.failTemplates[msg.TemplateName] || f.failRecipients[msg.Recipient] {
		return "", errors.New("ses send failed")
	}
	// The real sender renders the template before it calls SES, so a prop the
	// template needs but the job did not supply is a send failure. Rendering
	// here keeps that failure mode in the fake, which turns every job test
	// into a check that the job's props satisfy the template it names.
	if _, err := templates.Render(msg.TemplateName, msg.Props); err != nil {
		return "", err
	}
	renderedTemplates.record(msg.TemplateName)
	return "fake-message-id", nil
}

// renderedTemplates is the union of template names the job tests rendered
// successfully. TestMain checks it against the registry, so a template with a
// placeholder that no real call site supplies fails CI here instead of at
// send time.
var renderedTemplates = &templateCoverage{names: map[string]bool{}}

type templateCoverage struct {
	mu    sync.Mutex
	names map[string]bool
}

func (c *templateCoverage) record(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names[name] = true
}

// unrendered returns the registered templates no job test rendered.
func (c *templateCoverage) unrendered() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var missing []string
	for _, name := range templates.Names() {
		if !c.names[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func (f *fakeEmailSender) templates() []string {
	names := make([]string, 0, len(f.attempts))
	for _, msg := range f.attempts {
		names = append(names, msg.TemplateName)
	}
	return names
}

// htmlProp reads a prop that carries server-built markup. It must be a
// template.HTML: a plain string would be escaped into literal markup by the
// email template.
func htmlProp(t *testing.T, props map[string]interface{}, key string) string {
	t.Helper()
	v, ok := props[key].(template.HTML)
	if !ok {
		t.Fatalf("prop %q = %#v (%T), want template.HTML", key, props[key], props[key])
	}
	return string(v)
}

// trackedEvent is a recorded analytics call.
type trackedEvent struct {
	event      string
	distinctID string
	props      map[string]interface{}
}

// recordingTracker is a thread-safe analytics.Tracker fake.
type recordingTracker struct {
	mu     sync.Mutex
	events []trackedEvent
}

func (r *recordingTracker) Track(_ context.Context, event string, distinctID string, properties map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, trackedEvent{event: event, distinctID: distinctID, props: properties})
}

func (r *recordingTracker) last() *trackedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return nil
	}
	return &r.events[len(r.events)-1]
}

// withOutcome returns the recorded events whose "outcome" prop matches.
func (r *recordingTracker) withOutcome(outcome string) []trackedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []trackedEvent
	for _, e := range r.events {
		if e.props["outcome"] == outcome {
			matched = append(matched, e)
		}
	}
	return matched
}

// count returns the number of recorded events.
func (r *recordingTracker) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

const testModeratorsEmail = "moderators@test.local"

// errDBDown is the injected repository failure used by the cron job tests.
var errDBDown = errors.New("db down")

// jobsTestEnv bundles a worker server wired with fakes.
type jobsTestEnv struct {
	server   *Server
	handlers *Handlers
	repo     *fakeRepo
	sender   *fakeEmailSender
	tracker  *recordingTracker
}

// newJobsTestEnv wires an environment with APP_ENV=production (jobs run).
func newJobsTestEnv() *jobsTestEnv {
	return newJobsTestEnvWithConfig(func(*config.Config) {})
}

// newJobsTestEnvWithConfig lets tests mutate the config (app env,
// DEV_EMAIL_OVERRIDE, HIGHLIGHTED_MENTORS) before the handlers are built.
func newJobsTestEnvWithConfig(mutate func(cfg *config.Config)) *jobsTestEnv {
	cfg := testConfig()
	cfg.Email = config.EmailConfig{ModeratorsEmail: testModeratorsEmail}
	mutate(cfg)

	repo := newFakeRepo()
	sender := &fakeEmailSender{failTemplates: map[string]bool{}}
	tracker := &recordingTracker{}

	handlers := NewHandlers(repo, sender, tracker, cfg)
	server := NewServer(cfg, nil)
	server.RegisterJobRoutes(handlers)
	server.RegisterCronRoutes(handlers)

	return &jobsTestEnv{server: server, handlers: handlers, repo: repo, sender: sender, tracker: tracker}
}

// do performs a request against the wired worker server. body may be nil.
func (e *jobsTestEnv) do(method, path string, body []byte) *httptest.ResponseRecorder {
	var req = httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.server.Engine().ServeHTTP(w, req)
	return w
}

// testMentor returns a mentor fixture keyed under the given id.
func testMentor(id string) *JobMentor {
	return &JobMentor{
		ID:               id,
		LegacyID:         42,
		Name:             "John Doe",
		Email:            "john@example.com",
		Status:           "on_moderation",
		PreferredContact: "@johndoe",
		Slug:             "john-doe-42",
		JobTitle:         "Engineer",
		Workplace:        "Acme",
		Price:            "$50",
	}
}
