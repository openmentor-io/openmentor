package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/internal/services"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/openmentor-io/openmentor-api/pkg/metrics"
	"go.uber.org/zap"
)

type MCPHandler struct {
	service *services.MCPService
}

func NewMCPHandler(service *services.MCPService) *MCPHandler {
	return &MCPHandler{service: service}
}

// HandleMCPRequest handles MCP JSON-RPC 2.0 requests
func (h *MCPHandler) HandleMCPRequest(c *gin.Context) {
	start := time.Now()

	var req models.MCPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("Invalid MCP request format",
			zap.Error(err),
			zap.String("remote_addr", c.ClientIP()))

		metrics.MCPRequestTotal.WithLabelValues("invalid", "400").Inc()

		h.sendError(c, nil, models.ParseError, "Invalid JSON-RPC request", err.Error())
		return
	}

	// Validate JSON-RPC version
	if req.JSONRPC != "2.0" {
		logger.Warn("Invalid JSON-RPC version",
			zap.String("version", req.JSONRPC),
			zap.String("remote_addr", c.ClientIP()))

		metrics.MCPRequestTotal.WithLabelValues(req.Method, "400").Inc()

		h.sendError(c, req.ID, models.InvalidRequest, "Invalid JSON-RPC version", "Must be '2.0'")
		return
	}

	logger.Info("MCP request received",
		zap.String("method", req.Method),
		zap.Any("id", req.ID),
		zap.String("remote_addr", c.ClientIP()))

	// Track request duration
	defer func() {
		duration := metrics.MeasureDuration(start)
		metrics.MCPRequestDuration.WithLabelValues(req.Method).Observe(duration)
	}()

	// Route to appropriate handler
	switch req.Method {
	case "initialize":
		h.handleInitialize(c, req)
	case "tools/list":
		h.handleToolsList(c, req)
	case "tools/call":
		h.handleToolsCall(c, req)
	default:
		logger.Warn("Unknown MCP method",
			zap.String("method", req.Method),
			zap.String("remote_addr", c.ClientIP()))

		metrics.MCPRequestTotal.WithLabelValues(req.Method, "400").Inc()

		h.sendError(c, req.ID, models.MethodNotFound, "Method not found", fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

// handleInitialize responds to MCP initialization request
func (h *MCPHandler) handleInitialize(c *gin.Context, req models.MCPRequest) {
	logger.Info("MCP initialize request", zap.Any("params", req.Params))

	metrics.MCPRequestTotal.WithLabelValues("initialize", "200").Inc()

	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "openmentor-mcp-server",
			"version": "1.0.0",
		},
	}

	h.sendSuccess(c, req.ID, result)
}

// handleToolsList responds to tools list request
func (h *MCPHandler) handleToolsList(c *gin.Context, req models.MCPRequest) {
	logger.Info("MCP tools/list request")

	metrics.MCPRequestTotal.WithLabelValues("tools/list", "200").Inc()

	tools := h.service.GetAvailableTools()

	result := map[string]interface{}{
		"tools": tools,
	}

	h.sendSuccess(c, req.ID, result)
}

// handleToolsCall handles tool invocation
func (h *MCPHandler) handleToolsCall(c *gin.Context, req models.MCPRequest) {
	// Extract tool name from params
	toolName, ok := req.Params["name"].(string)
	if !ok {
		logger.Warn("Missing tool name in tools/call request")

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()

		h.sendError(c, req.ID, models.InvalidParams, "Missing tool name", "Parameter 'name' is required")
		return
	}

	// Extract tool arguments
	toolArgs, ok := req.Params["arguments"].(map[string]interface{})
	if !ok {
		toolArgs = make(map[string]interface{})
	}

	logger.Info("MCP tools/call request",
		zap.String("tool", toolName),
		zap.Any("arguments", toolArgs),
		zap.String("remote_addr", c.ClientIP()))

	// Route to appropriate tool handler
	switch toolName {
	case "list_mentors":
		h.handleListMentors(c, req.ID, toolArgs)
	case "get_mentor":
		h.handleGetMentor(c, req.ID, toolArgs)
	case "search_mentors":
		h.handleSearchMentors(c, req.ID, toolArgs)
	default:
		logger.Warn("Unknown tool requested",
			zap.String("tool", toolName),
			zap.String("remote_addr", c.ClientIP()))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues(toolName, "error").Inc()

		h.sendError(c, req.ID, models.MethodNotFound, "Tool not found", fmt.Sprintf("Unknown tool: %s", toolName))
	}
}

// handleListMentors handles the list_mentors tool
func (h *MCPHandler) handleListMentors(c *gin.Context, id interface{}, args map[string]interface{}) {
	start := time.Now()

	var params models.ListMentorsParams
	if err := services.ParseParams(args, &params); err != nil {
		logger.Warn("Invalid list_mentors parameters",
			zap.Error(err),
			zap.Any("args", args))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues("list_mentors", "error").Inc()

		h.sendError(c, id, models.InvalidParams, "Invalid parameters", err.Error())
		return
	}

	result, err := h.service.ListMentors(c.Request.Context(), &params)
	if err != nil {
		logger.Error("Failed to list mentors",
			zap.Error(err),
			zap.Any("params", params))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues("list_mentors", "error").Inc()

		h.sendError(c, id, models.InternalError, "Failed to list mentors", err.Error())
		return
	}

	// Track metrics
	duration := metrics.MeasureDuration(start)
	metrics.MCPRequestTotal.WithLabelValues("tools/call", "200").Inc()
	metrics.MCPToolInvocations.WithLabelValues("list_mentors", "success").Inc()
	metrics.MCPResultsReturned.WithLabelValues("list_mentors").Observe(float64(result.Count))

	logger.Info("list_mentors completed",
		zap.Int("count", result.Count),
		zap.Float64("duration_seconds", duration),
		zap.Any("filters", params))

	structuredContent := map[string]interface{}{
		"mentors": result.Mentors,
		"count":   result.Count,
	}

	// Format as MCP tool result
	toolResult := map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%s", structuredContent),
			},
		},
		"isError":           false,
		"structuredContent": structuredContent,
	}

	h.sendSuccess(c, id, toolResult)
}

// handleGetMentor handles the get_mentor tool
func (h *MCPHandler) handleGetMentor(c *gin.Context, id interface{}, args map[string]interface{}) {
	start := time.Now()

	var params models.GetMentorParams
	if err := services.ParseParams(args, &params); err != nil {
		logger.Warn("Invalid get_mentor parameters",
			zap.Error(err),
			zap.Any("args", args))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues("get_mentor", "error").Inc()

		h.sendError(c, id, models.InvalidParams, "Invalid parameters", err.Error())
		return
	}

	result, err := h.service.GetMentor(c.Request.Context(), &params)
	if err != nil {
		logger.Error("Failed to get mentor",
			zap.Error(err),
			zap.Any("params", params))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues("get_mentor", "error").Inc()

		h.sendError(c, id, models.InternalError, "Failed to get mentor", err.Error())
		return
	}

	// Track metrics
	duration := metrics.MeasureDuration(start)
	metrics.MCPRequestTotal.WithLabelValues("tools/call", "200").Inc()
	metrics.MCPToolInvocations.WithLabelValues("get_mentor", "success").Inc()

	if result.Mentor != nil {
		metrics.MCPResultsReturned.WithLabelValues("get_mentor").Observe(1)
		logger.Info("get_mentor completed",
			zap.Int("mentor_id", result.Mentor.ID),
			zap.String("mentor_slug", result.Mentor.Slug),
			zap.Float64("duration_seconds", duration))
	} else {
		metrics.MCPResultsReturned.WithLabelValues("get_mentor").Observe(0)
		logger.Info("get_mentor completed - not found",
			zap.Float64("duration_seconds", duration),
			zap.Any("params", params))
	}

	structuredContent := map[string]interface{}{
		"mentor": result.Mentor,
	}

	// Format as MCP tool result
	var toolResult map[string]interface{}
	if result.Mentor != nil {
		toolResult = map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("%s", structuredContent),
				},
			},
			"isError":           false,
			"structuredContent": structuredContent,
		}
	} else {
		toolResult = map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "Mentor not found.",
				},
			},
			"isError": false,
			"structuredContent": map[string]interface{}{
				"mentor": nil,
			},
		}
	}

	h.sendSuccess(c, id, toolResult)
}

// handleSearchMentors handles the search_mentors tool
func (h *MCPHandler) handleSearchMentors(c *gin.Context, id interface{}, args map[string]interface{}) {
	start := time.Now()

	var params models.SearchMentorsParams
	if err := services.ParseParams(args, &params); err != nil {
		logger.Warn("Invalid search_mentors parameters",
			zap.Error(err),
			zap.Any("args", args))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues("search_mentors", "error").Inc()

		h.sendError(c, id, models.InvalidParams, "Invalid parameters", err.Error())
		return
	}

	result, err := h.service.SearchMentors(c.Request.Context(), &params)
	if err != nil {
		logger.Error("Failed to search mentors",
			zap.Error(err),
			zap.Any("params", params))

		metrics.MCPRequestTotal.WithLabelValues("tools/call", "400").Inc()
		metrics.MCPToolInvocations.WithLabelValues("search_mentors", "error").Inc()

		h.sendError(c, id, models.InternalError, "Failed to search mentors", err.Error())
		return
	}

	// Track keyword count metrics
	keywordCount := len(strings.Fields(params.Query))
	keywordRange := getKeywordRange(keywordCount)
	metrics.MCPSearchKeywords.WithLabelValues(keywordRange).Inc()

	// Track metrics
	duration := metrics.MeasureDuration(start)
	metrics.MCPRequestTotal.WithLabelValues("tools/call", "200").Inc()
	metrics.MCPToolInvocations.WithLabelValues("search_mentors", "success").Inc()
	metrics.MCPResultsReturned.WithLabelValues("search_mentors").Observe(float64(result.Count))

	logger.Info("search_mentors completed",
		zap.String("query", params.Query),
		zap.Int("keyword_count", keywordCount),
		zap.Int("results", result.Count),
		zap.Float64("duration_seconds", duration),
		zap.Any("filters", params))

	structuredContent := map[string]interface{}{
		"mentors": result.Mentors,
		"count":   result.Count,
		"query":   params.Query,
	}

	// Format as MCP tool result
	toolResult := map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("%s", structuredContent),
			},
		},
		"isError":           false,
		"structuredContent": structuredContent,
	}

	h.sendSuccess(c, id, toolResult)
}

// sendSuccess sends a successful JSON-RPC response
func (h *MCPHandler) sendSuccess(c *gin.Context, id interface{}, result interface{}) {
	response := models.MCPResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
	c.JSON(http.StatusOK, response)
}

// sendError sends a JSON-RPC error response
func (h *MCPHandler) sendError(c *gin.Context, id interface{}, code int, message string, data interface{}) {
	response := models.MCPResponse{
		JSONRPC: "2.0",
		Error: &models.MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: id,
	}

	// Use appropriate HTTP status code based on error type
	httpStatus := http.StatusOK // JSON-RPC errors are still 200 OK
	switch code {
	case models.ParseError:
		httpStatus = http.StatusBadRequest
	case models.InvalidRequest:
		httpStatus = http.StatusBadRequest
	}

	c.JSON(httpStatus, response)
}

// getKeywordRange returns a range label for keyword count metrics
func getKeywordRange(count int) string {
	switch {
	case count <= 2:
		return "1-2"
	case count <= 5:
		return "3-5"
	case count <= 10:
		return "6-10"
	default:
		return "10+"
	}
}
