package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

// WorkerTokenHeader is the shared-secret header the background worker's
// /jobs/* middleware validates (internal/worker.AuthMiddleware). Both
// CallAsync and CallAsyncWithPayload send it when an auth token is
// configured (WORKER_AUTH_TOKEN); when the token is empty no header is
// sent, matching the worker's allow-when-unset behavior.
const WorkerTokenHeader = "X-Worker-Token" //nolint:gosec // header name, not a credential

// CallAsync calls a trigger URL asynchronously with the record id appended
// verbatim to the URL (so GET-style trigger URLs must end with "?param=").
// This is used to notify the background worker after database operations.
// Failures are logged but don't block the operation.
//
// ctx carries the caller's trace context so the worker's span joins the
// same trace (the instrumented httpclient injects the W3C traceparent).
// The goroutine outlives the caller's HTTP request, so cancellation is
// stripped via context.WithoutCancel: values (trace context) propagate,
// but the parent request finishing does not abort the trigger call.
func CallAsync(ctx context.Context, triggerURL, recordID, authToken string, httpClient httpclient.Client) {
	if triggerURL == "" {
		// No trigger URL configured, skip silently
		return
	}
	ctx = context.WithoutCancel(ctx)

	// Run in goroutine to avoid blocking
	go func() {
		targetURL := fmt.Sprintf("%s%s", triggerURL, recordID)

		logger.Info("Calling trigger URL",
			zap.String("url", targetURL),
			zap.String("record_id", recordID))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			logger.Error("Failed to build trigger request",
				zap.Error(err),
				zap.String("url", targetURL),
				zap.String("record_id", recordID))
			return
		}
		if authToken != "" {
			req.Header.Set(WorkerTokenHeader, authToken)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Error("Failed to call trigger URL",
				zap.Error(err),
				zap.String("url", targetURL),
				zap.String("record_id", recordID))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Info("Trigger URL called successfully",
				zap.String("url", targetURL),
				zap.String("record_id", recordID),
				zap.Int("status_code", resp.StatusCode))
		} else {
			logger.Warn("Trigger URL returned non-success status",
				zap.String("url", targetURL),
				zap.String("record_id", recordID),
				zap.Int("status_code", resp.StatusCode))
		}
	}()
}

// CallAsyncWithPayload calls a trigger URL asynchronously with a JSON
// payload (POST). This is used for triggers that need more than just a
// record ID. Failures are logged but don't block the operation.
//
// See CallAsync for the ctx semantics (trace propagation without the
// caller's cancellation).
func CallAsyncWithPayload(ctx context.Context, triggerURL string, payload interface{}, authToken string, httpClient httpclient.Client) {
	if triggerURL == "" {
		// No trigger URL configured, skip silently
		return
	}
	ctx = context.WithoutCancel(ctx)

	// Run in goroutine to avoid blocking
	go func() {
		jsonData, err := json.Marshal(payload)
		if err != nil {
			logger.Error("Failed to marshal trigger payload",
				zap.Error(err),
				zap.String("url", triggerURL))
			return
		}

		logger.Info("Calling trigger URL with payload",
			zap.String("url", triggerURL))

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, triggerURL, bytes.NewBuffer(jsonData))
		if err != nil {
			logger.Error("Failed to build trigger request",
				zap.Error(err),
				zap.String("url", triggerURL))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if authToken != "" {
			req.Header.Set(WorkerTokenHeader, authToken)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Error("Failed to call trigger URL",
				zap.Error(err),
				zap.String("url", triggerURL))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			logger.Info("Trigger URL called successfully",
				zap.String("url", triggerURL),
				zap.Int("status_code", resp.StatusCode))
		} else {
			logger.Warn("Trigger URL returned non-success status",
				zap.String("url", triggerURL),
				zap.Int("status_code", resp.StatusCode))
		}
	}()
}
