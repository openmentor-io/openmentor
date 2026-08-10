package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// browserAddress is the sentinel. RFC 2606, so nothing here or in a CI failure
// message is a real address.
const browserAddress = "mentee.person@example.com"

// TestFrontendLogsAreRedactedOnTheWayToDisk covers the one sink no logger-level
// fix can reach: /logs writes browser-supplied lines to frontend.log with
// encoding/json, and Alloy tails that file straight into Loki. It is also the
// least trusted sink in the system — the endpoint takes whatever is POSTed.
func TestFrontendLogsAreRedactedOnTheWayToDisk(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The handler logs a count through the global logger, which this package's
	// tests do not otherwise initialize.
	previous := logger.Log
	logger.Log = zap.NewNop()
	t.Cleanup(func() { logger.Log = previous })

	logDir := t.TempDir()
	handler := NewLogsHandler(logDir)

	router := gin.New()
	router.POST("/logs", handler.ReceiveFrontendLogs)

	body, err := json.Marshal(map[string]interface{}{
		"logs": []map[string]interface{}{
			{
				"timestamp": "2026-08-07T10:00:00Z",
				"level":     "error",
				"message":   "contact form failed for " + browserAddress,
				"context": map[string]interface{}{
					// PII in a context value.
					"email": browserAddress,
					// A capability under an innocuous name — the shape rule, not
					// the key rule, is what catches this one.
					"path": "/api/v1/reviews/11111111-2222-4333-8444-555555555555",
					// A credential by key name.
					"login_token": "abc123",
					// Something an operator actually needs to survive.
					"status": "500",
					"count":  3,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	written, err := os.ReadFile(filepath.Join(logDir, "frontend.log"))
	if err != nil {
		t.Fatalf("read frontend.log: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("frontend.log is empty: the test wrote nothing and proves nothing")
	}

	contents := string(written)
	if strings.Contains(contents, browserAddress) {
		t.Errorf("frontend.log carries the raw address: %s", contents)
	}
	if strings.Contains(contents, "abc123") {
		t.Errorf("frontend.log carries a credential-shaped context value: %s", contents)
	}
	if strings.Contains(contents, "11111111-2222-4333-8444-555555555555") {
		t.Errorf("frontend.log carries a review capability id: %s", contents)
	}

	// And the line is still usable.
	var line map[string]interface{}
	scanner := bufio.NewScanner(bytes.NewReader(written))
	if !scanner.Scan() {
		t.Fatal("frontend.log has no line to decode")
	}
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		t.Fatalf("frontend.log is no longer valid JSON, which breaks the Alloy parse stage: %v", err)
	}
	if line["level"] != "error" || line["service"] != "nextjs" {
		t.Errorf("the line lost its routing fields: %v", line)
	}
	if line["status"] != "500" {
		t.Errorf("status = %v, want it kept: over-redaction makes the sink useless", line["status"])
	}
	if line["count"] != float64(3) {
		t.Errorf("count = %v, want a number passed through", line["count"])
	}
	if line["login_token"] != redact.Placeholder {
		t.Errorf("login_token = %v, want %q — knowing the field was present is the useful part", line["login_token"], redact.Placeholder)
	}
}

// TestFrontendLogContextRejectsNestedStructures pins the deliberate choice not to
// walk an untrusted nested object on the write path.
func TestFrontendLogContextRejectsNestedStructures(t *testing.T) {
	nested := map[string]interface{}{"inner": browserAddress}
	if got := redactContextValue("details", nested); got != redact.Placeholder {
		t.Errorf("redactContextValue(nested) = %v, want %q", got, redact.Placeholder)
	}
}
