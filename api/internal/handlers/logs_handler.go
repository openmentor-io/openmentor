package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"go.uber.org/zap"
)

type LogsHandler struct {
	logDir string
	mu     sync.Mutex
}

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
	return &LogsHandler{
		logDir: logDir,
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
	h.mu.Lock()
	defer h.mu.Unlock()

	// Ensure log directory exists
	//nolint:gosec // G301: 0755 is appropriate for log directory to allow group/other read
	if err := os.MkdirAll(h.logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open frontend log file in append mode
	logPath := filepath.Join(h.logDir, "frontend.log")
	//nolint:gosec // G302: 0644 is appropriate for log files (group/other read for log aggregators)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open frontend log file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	// Write each log entry as a JSON line
	encoder := json.NewEncoder(f)
	for _, entry := range logs {
		// Reformat log entry to match backend format
		logLine := map[string]interface{}{
			"ts":      entry.Timestamp,
			"level":   entry.Level,
			"msg":     entry.Message,
			"service": "nextjs",
		}

		// Add context fields if present
		if entry.Context != nil {
			for k, v := range entry.Context {
				logLine[k] = v
			}
		}

		if err := encoder.Encode(logLine); err != nil {
			return fmt.Errorf("failed to encode log entry: %w", err)
		}
	}

	return nil
}
