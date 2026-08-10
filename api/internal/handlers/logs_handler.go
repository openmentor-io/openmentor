package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// redactContextValue applies the log-field rules to one browser-supplied context
// entry. Only strings are rewritten; a nested object is dropped outright rather
// than walked, because an unbounded structure from an untrusted client is not
// something to recurse over inside a mutex on the write path.
func redactContextValue(key string, value interface{}) interface{} {
	if redact.SensitiveKey(key) {
		return redact.Placeholder
	}
	switch typed := value.(type) {
	case string:
		return redact.Text(typed)
	case nil, bool, float64, json.Number:
		return typed
	default:
		return redact.Placeholder
	}
}

type LogsHandler struct {
	// writer is a lumberjack.Logger, so frontend.log is bounded the same way
	// pkg/logger bounds app.log and error.log. Before this it was a bare
	// O_APPEND open per request and grew without limit — the one log file in the
	// deployment with no rotation, filling the same volume the rotated ones
	// share.
	writer io.WriteCloser
	mu     sync.Mutex
}

// frontendLogRotation mirrors pkg/logger's app.log/error.log settings, so every
// file on the log volume is bounded by the same rule.
//
// frontend.log is deliberately NOT one of Alloy's tail targets (it reads
// /var/log/backend/app.log and /var/log/frontend/app-*.log), so rotating it
// changes nothing about what reaches Loki. It is disk, and only disk.
const (
	frontendLogMaxSizeMB  = 100
	frontendLogMaxBackups = 7
	frontendLogMaxAgeDays = 7
)

type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

type LogBatchRequest struct {
	Logs []LogEntry `json:"logs" binding:"required,max=100,dive"`
}

func NewLogsHandler(logDir string) *LogsHandler {
	return newLogsHandler(logDir, frontendLogMaxSizeMB)
}

// newLogsHandler exists so the rotation test can choose a size it can actually
// write past; production always uses frontendLogMaxSizeMB.
func newLogsHandler(logDir string, maxSizeMB int) *LogsHandler {
	return &LogsHandler{
		writer: &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "frontend.log"),
			MaxSize:    maxSizeMB,
			MaxBackups: frontendLogMaxBackups,
			MaxAge:     frontendLogMaxAgeDays,
			Compress:   true,
		},
	}
}

func (h *LogsHandler) ReceiveFrontendLogs(c *gin.Context) {
	var req LogBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if len(req.Logs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No logs provided"})
		return
	}

	// Write logs to frontend.log file
	if err := h.writeLogsToFile(req.Logs); err != nil {
		logger.Error("Failed to write frontend logs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write logs"})
		return
	}

	logger.Info("Received frontend logs", zap.Int("count", len(req.Logs)))
	c.JSON(http.StatusOK, gin.H{"success": true, "received": len(req.Logs)})
}

func (h *LogsHandler) writeLogsToFile(logs []LogEntry) error {
	// lumberjack.Logger.Write is itself serialized, but the mutex still has to
	// hold for the whole batch: without it two concurrent batches interleave
	// their JSON lines mid-encode.
	h.mu.Lock()
	defer h.mu.Unlock()

	// No MkdirAll and no OpenFile: lumberjack creates the directory and the file
	// on first write, and reopens it after each rotation.
	encoder := json.NewEncoder(h.writer)

	for _, entry := range logs {
		// Reformat log entry to match backend format
		logLine := map[string]interface{}{
			"ts":      entry.Timestamp,
			"level":   entry.Level,
			"msg":     redact.Text(entry.Message),
			"service": "nextjs",
		}

		// Add context fields if present
		if entry.Context != nil {
			for k, v := range entry.Context {
				logLine[k] = redactContextValue(k, v)
			}
		}

		if err := encoder.Encode(logLine); err != nil {
			return fmt.Errorf("failed to encode log entry: %w", err)
		}
	}

	return nil
}
