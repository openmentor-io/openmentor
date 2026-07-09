package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/openmentor-io/openmentor-api/pkg/metrics"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	logger.Log = zap.NewNop()
	metrics.Init("openmentor-worker-test")
	os.Exit(m.Run())
}

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			GinMode: gin.TestMode,
			AppEnv:  "production",
		},
		Worker: config.WorkerConfig{
			Port:        "8090",
			CronEnabled: true,
		},
	}
}

func performRequest(engine *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// TestJobRoutesRegistered verifies every ported endpoint is reachable with
// the methods the func app supported (a handler response, not gin's bare
// 404/405 for unregistered routes).
func TestJobRoutesRegistered(t *testing.T) {
	env := newJobsTestEnv()

	routes := []struct {
		method string
		path   string
		want   int
	}{
		// Records don't exist in the empty fake repo -> handler 404 JSON.
		{http.MethodPost, "/jobs/new-mentor-watcher?mentorId=abc", http.StatusNotFound},
		{http.MethodGet, "/jobs/new-mentor-watcher?mentorId=abc", http.StatusNotFound},
		{http.MethodPost, "/jobs/new-request-watcher?requestId=abc", http.StatusNotFound},
		{http.MethodGet, "/jobs/new-request-watcher?requestId=abc", http.StatusNotFound},
		// JSON handlers reject an empty body as an invalid payload -> 400.
		{http.MethodPost, "/jobs/mentor-login-email", http.StatusBadRequest},
		{http.MethodPost, "/jobs/moderator-login-email", http.StatusBadRequest},
		{http.MethodPost, "/jobs/mentor-moderation-action", http.StatusBadRequest},
		{http.MethodPost, "/jobs/process-mentee-review?reviewId=abc", http.StatusNotFound},
		{http.MethodGet, "/jobs/process-mentee-review?reviewId=abc", http.StatusNotFound},
		{http.MethodGet, "/jobs/request-process-finished?requestId=abc", http.StatusNotFound},
	}

	for _, r := range routes {
		w := env.do(r.method, r.path, nil)
		if w.Code != r.want {
			t.Errorf("%s %s = %d, want %d", r.method, r.path, w.Code, r.want)
		}
		if w.Body.Len() == 0 {
			t.Errorf("%s %s returned an empty body, want handler JSON", r.method, r.path)
		}
	}
}

func TestHealthzWithoutPool(t *testing.T) {
	s := NewServer(testConfig(), nil)

	w := performRequest(s.Engine(), http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want 200", w.Code)
	}
}

func TestJobsRequireWorkerTokenWhenConfigured(t *testing.T) {
	cfg := testConfig()
	cfg.Worker.AuthToken = "secret-token"
	s := NewServer(cfg, nil)
	s.RegisterJobRoutes(NewHandlers(newFakeRepo(), &fakeEmailSender{}, &recordingTracker{}, cfg))

	// Missing token -> 401
	w := performRequest(s.Engine(), http.MethodPost, "/jobs/mentor-login-email", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d, want 401", w.Code)
	}

	// Wrong token -> 401
	w = performRequest(s.Engine(), http.MethodPost, "/jobs/mentor-login-email",
		map[string]string{WorkerTokenHeader: "wrong"})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", w.Code)
	}

	// Correct token -> reaches the handler (400: empty body is an invalid payload)
	w = performRequest(s.Engine(), http.MethodPost, "/jobs/mentor-login-email",
		map[string]string{WorkerTokenHeader: "secret-token"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("correct token: status = %d, want 400", w.Code)
	}

	// Healthz stays unauthenticated
	w = performRequest(s.Engine(), http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("healthz with auth configured: status = %d, want 200", w.Code)
	}
}

func TestJobsAllowedWhenNoTokenConfigured(t *testing.T) {
	env := newJobsTestEnv()

	w := env.do(http.MethodPost, "/jobs/mentor-login-email", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("no token configured: status = %d, want 400", w.Code)
	}
}

func TestNewCronRegistersFourJobs(t *testing.T) {
	env := newJobsTestEnv()

	c, err := NewCron(env.handlers)
	if err != nil {
		t.Fatalf("NewCron failed (invalid schedule expression?): %v", err)
	}
	if got := len(c.Entries()); got != 4 {
		t.Errorf("registered %d cron entries, want 4", got)
	}
}

func TestRunCronJobReturnsSummary(t *testing.T) {
	summary, err := runCronJob(context.Background(), "test-job", func(ctx context.Context) (JobSummary, error) {
		return JobSummary{Job: "test-job", MentorsMatched: 3, EmailsSent: 2, EmailFailures: 1}, nil
	})
	if err != nil {
		t.Fatalf("runCronJob returned error: %v", err)
	}
	if summary.MentorsMatched != 3 || summary.EmailsSent != 2 || summary.EmailFailures != 1 {
		t.Errorf("summary not passed through: %+v", summary)
	}
}

func TestRunCronJobRecoversFromPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("panic escaped the cron wrapper: %v", recovered)
		}
	}()

	_, err := runCronJob(context.Background(), "panicking-job", func(ctx context.Context) (JobSummary, error) {
		panic("boom")
	})
	if err == nil {
		t.Error("a panicking job should surface as an error")
	}
}

func TestRunCronJobHandlesJobError(t *testing.T) {
	// Must not panic or escape; error is logged, counted and returned.
	_, err := runCronJob(context.Background(), "failing-job", func(ctx context.Context) (JobSummary, error) {
		return JobSummary{Job: "failing-job"}, errors.New("job failed")
	})
	if err == nil {
		t.Error("job error should be returned to the caller")
	}
}

func TestRunCronJobPropagatesSkip(t *testing.T) {
	summary, err := runCronJob(context.Background(), "skipped-job", func(ctx context.Context) (JobSummary, error) {
		return JobSummary{Job: "skipped-job", Skipped: true}, nil
	})
	if err != nil {
		t.Fatalf("runCronJob returned error: %v", err)
	}
	if !summary.Skipped {
		t.Error("skip flag should be passed through to the caller")
	}
}
