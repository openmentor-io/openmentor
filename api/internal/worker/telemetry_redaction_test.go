package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// workerReviewCapability is a live client_requests id — the review flow treats
// it as a bearer token, so the worker must not put it in analytics or logs (P14).
const workerReviewCapability = "11111111-2222-4333-8444-555555555555"

// TestJobTelemetryOmitsRequestCapability drives the two request-shaped jobs with
// a sentinel id in the query string and checks every sink they write to.
func TestJobTelemetryOmitsRequestCapability(t *testing.T) {
	jobs := []struct {
		name string
		path string
	}{
		{"request-process-finished", "/jobs/request-process-finished?requestId=" + workerReviewCapability},
		{"new-request-watcher", "/jobs/new-request-watcher?requestId=" + workerReviewCapability},
	}

	for _, job := range jobs {
		t.Run(job.name, func(t *testing.T) {
			logs := observeLogs(t)

			env := newJobsTestEnv()
			env.repo.requestsWithMentor[workerReviewCapability] = finishedRequest("done")
			env.repo.requests[workerReviewCapability] = finishedRequest("new")
			env.repo.mentors["m1"] = testMentor("m1")

			if w := env.do(http.MethodGet, job.path, nil); w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}

			if env.tracker.count() == 0 {
				t.Fatal("no analytics events recorded: the test drove nothing")
			}
			for _, event := range env.tracker.events {
				if strings.Contains(event.distinctID, workerReviewCapability) {
					t.Errorf("%s distinct_id leaked the capability: %q", event.event, event.distinctID)
				}
				if strings.Contains(fmt.Sprint(event.props), workerReviewCapability) {
					t.Errorf("%s properties leaked the capability: %v", event.event, event.props)
				}
			}

			if leaked := renderedLogs(logs); strings.Contains(leaked, workerReviewCapability) {
				t.Errorf("log entry leaked the capability: %s", leaked)
			}
		})
	}
}

// observeLogs swaps the global logger for a recording one.
func observeLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Log
	logger.Log = zap.New(core)
	t.Cleanup(func() { logger.Log = previous })

	return logs
}

// renderedLogs flattens every recorded entry into one searchable string.
func renderedLogs(logs *observer.ObservedLogs) string {
	var out strings.Builder
	for _, entry := range logs.All() {
		out.WriteString(entry.Message + " " + fmt.Sprint(entry.ContextMap()) + "\n")
	}
	return out.String()
}

// TestJobLogsRedactErrorText covers the error TEXT rather than the fields. The
// repository named the row it failed on, so zap.Error wrote the raw id right
// next to the hashed request_ref; Postgres does the same unprompted (a
// constraint violation reports `Key (id)=(<uuid>)`), which is why the log site
// redacts instead of trusting the message it is handed.
func TestJobLogsRedactErrorText(t *testing.T) {
	// Verbatim shape of a wrapped repository error that names the row.
	fetchErr := fmt.Errorf("failed to fetch client request %s: %w", workerReviewCapability, errors.New("conn closed"))

	cases := map[string]struct {
		path    string
		inject  func(*fakeRepo)
		wantLog string
	}{
		"new_request_watcher_fetch": {
			path:    "/jobs/new-request-watcher?requestId=" + workerReviewCapability,
			inject:  func(f *fakeRepo) { f.requestErr = fetchErr },
			wantLog: "Failed to fetch request",
		},
		"new_request_watcher_update": {
			path: "/jobs/new-request-watcher?requestId=" + workerReviewCapability,
			inject: func(f *fakeRepo) {
				f.requests[workerReviewCapability] = finishedRequest("new")
				f.setRequestErr = fmt.Errorf("failed to update client request %s: %w",
					workerReviewCapability, errors.New("deadlock detected"))
			},
			wantLog: "Failed to update request",
		},
		"request_process_finished_fetch": {
			path:    "/jobs/request-process-finished?requestId=" + workerReviewCapability,
			inject:  func(f *fakeRepo) { f.requestErr = fetchErr },
			wantLog: "Failed to fetch request",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := observeLogs(t)
			env := newJobsTestEnv()
			tc.inject(env.repo)

			if w := env.do(http.MethodGet, tc.path, nil); w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", w.Code)
			}

			out := renderedLogs(logs)
			if !strings.Contains(out, tc.wantLog) {
				t.Fatalf("the log site moved, this case covers nothing: %s", out)
			}
			if strings.Contains(out, workerReviewCapability) {
				t.Errorf("log entry leaked the capability: %s", out)
			}
			// Correlation survives: the hashed ref still identifies the row.
			if !strings.Contains(out, redact.ID(workerReviewCapability)) {
				t.Errorf("log entry carries no hashed reference: %s", out)
			}
		})
	}
}

// TestRepositoryErrorsDoNotNameTheRequest pins the source side: the wrapped
// error must not carry the id at all, so it stays out of every sink it reaches
// (log line, PostHog error tracking) and not only the ones that redact.
//
// The pool points at a closed port, so the query fails while connecting and the
// wrapping under test runs without a database.
func TestRepositoryErrorsDoNotNameTheRequest(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://openmentor@127.0.0.1:1/openmentor?connect_timeout=2&sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	repo := NewRepository(pool)

	cases := map[string]func() error{
		"GetJobRequestByID": func() error {
			_, err := repo.GetJobRequestByID(ctx, workerReviewCapability)
			return err
		},
		"GetJobRequestWithMentorName": func() error {
			_, err := repo.GetJobRequestWithMentorName(ctx, workerReviewCapability)
			return err
		},
		"SetRequestContactPending": func() error {
			_, err := repo.SetRequestContactPending(ctx, workerReviewCapability, "@handle")
			return err
		},
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("query succeeded: the error path is no longer exercised")
			}
			if strings.Contains(err.Error(), workerReviewCapability) {
				t.Errorf("repository error embeds the request capability: %v", err)
			}
		})
	}
}
