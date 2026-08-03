package worker

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
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
			core, logs := observer.New(zap.DebugLevel)
			previous := logger.Log
			logger.Log = zap.New(core)
			t.Cleanup(func() { logger.Log = previous })

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

			for _, entry := range logs.All() {
				rendered := entry.Message + " " + fmt.Sprint(entry.ContextMap())
				if strings.Contains(rendered, workerReviewCapability) {
					t.Errorf("log entry leaked the capability: %s", rendered)
				}
			}
		})
	}
}
