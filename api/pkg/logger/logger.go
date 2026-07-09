package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	Log *zap.Logger
)

// Config holds logger configuration
type Config struct {
	Level       string
	LogDir      string
	Environment string
	ServiceName string
}

// Initialize sets up the global logger
func Initialize(cfg Config) error {
	var config zap.Config

	// Determine log level
	level := zapcore.InfoLevel
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return fmt.Errorf("invalid log level %s: %w", cfg.Level, err)
	}

	if cfg.Environment == "development" {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	config.Level = zap.NewAtomicLevelAt(level)

	var logger *zap.Logger
	var err error

	// Configure logging with rotation in production
	if cfg.Environment == "production" && cfg.LogDir != "" {
		// Ensure log directory exists
		if mkdirErr := os.MkdirAll(cfg.LogDir, 0o755); mkdirErr != nil {
			return fmt.Errorf("failed to create log directory: %w", mkdirErr)
		}

		// Configure log rotation with lumberjack
		appLogWriter := &lumberjack.Logger{
			Filename:   filepath.Join(cfg.LogDir, "app.log"),
			MaxSize:    100, // MB
			MaxBackups: 7,
			MaxAge:     7, // days
			Compress:   true,
		}

		errorLogWriter := &lumberjack.Logger{
			Filename:   filepath.Join(cfg.LogDir, "error.log"),
			MaxSize:    100, // MB
			MaxBackups: 7,
			MaxAge:     7, // days
			Compress:   true,
		}

		// Create core with multiple outputs
		encoder := zapcore.NewJSONEncoder(config.EncoderConfig)

		// stdout for all logs
		stdoutCore := zapcore.NewCore(
			encoder,
			zapcore.AddSync(os.Stdout),
			level,
		)

		// File for all logs
		appFileCore := zapcore.NewCore(
			encoder,
			zapcore.AddSync(appLogWriter),
			level,
		)

		// File for errors only
		errorFileCore := zapcore.NewCore(
			encoder,
			zapcore.AddSync(errorLogWriter),
			zapcore.ErrorLevel,
		)

		// Combine all cores
		core := zapcore.NewTee(stdoutCore, appFileCore, errorFileCore)

		// Build logger
		logger = zap.New(
			core,
			zap.AddCaller(),
			zap.AddCallerSkip(1),
			zap.AddStacktrace(zapcore.ErrorLevel),
		)
	} else {
		// Development: just stdout/stderr
		config.OutputPaths = []string{"stdout"}
		config.ErrorOutputPaths = []string{"stderr"}

		logger, err = config.Build(
			zap.AddCallerSkip(1),
			zap.AddStacktrace(zapcore.ErrorLevel),
		)
		if err != nil {
			return fmt.Errorf("failed to build logger: %w", err)
		}
	}

	// Add service_name as a default field to all log entries
	// This makes logs self-describing and easier to debug
	if cfg.ServiceName != "" {
		logger = logger.With(zap.String("service_name", cfg.ServiceName))
	}

	Log = logger
	return nil
}

// Info logs an info message
func Info(msg string, fields ...zap.Field) {
	Log.Info(msg, fields...)
}

// Debug logs a debug message
func Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, fields...)
}

// Warn logs a warning message
func Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, fields...)
}

// Error logs an error message
func Error(msg string, fields ...zap.Field) {
	Log.Error(msg, fields...)
}

// Fatal logs a fatal message and exits
func Fatal(msg string, fields ...zap.Field) {
	Log.Fatal(msg, fields...)
}

// With creates a child logger with additional fields
func With(fields ...zap.Field) *zap.Logger {
	return Log.With(fields...)
}

// Sync flushes any buffered log entries
func Sync() {
	_ = Log.Sync() //nolint:errcheck // Best-effort sync on exit, failure is acceptable
}

// extractTraceContext extracts trace ID and span ID from context and returns zap fields
func extractTraceContext(ctx context.Context) []zap.Field {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}

	spanContext := span.SpanContext()
	return []zap.Field{
		zap.String("trace_id", spanContext.TraceID().String()),
		zap.String("span_id", spanContext.SpanID().String()),
		zap.String("trace_flags", spanContext.TraceFlags().String()),
	}
}

// LogHTTPRequest logs an HTTP request with standard fields including trace context
func LogHTTPRequest(ctx context.Context, method, path string, statusCode int, duration float64, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status", statusCode),
		zap.Float64("duration", duration),
	}

	// Add trace context if available
	if traceFields := extractTraceContext(ctx); traceFields != nil {
		baseFields = append(baseFields, traceFields...)
	}

	baseFields = append(baseFields, fields...)

	switch statusCode {
	case 401:
		Info("HTTP request unauthorized", baseFields...)
	case 403:
		Info("HTTP request forbidden", baseFields...)
	case 404:
		Info("HTTP request not found", baseFields...)
	case 429:
		Warn("HTTP request rate limited", baseFields...)
	default:
		switch {
		case statusCode >= 500:
			Error("HTTP request failed", baseFields...)
		case statusCode >= 400:
			Warn("HTTP request client error", baseFields...)
		default:
			Info("HTTP request", baseFields...)
		}
	}
}

// LogAPICall logs an external API call including trace context
func LogAPICall(ctx context.Context, service, operation, status string, duration float64, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("service", service),
		zap.String("operation", operation),
		zap.String("status", status),
		zap.Float64("duration", duration),
	}

	// Add trace context if available
	if traceFields := extractTraceContext(ctx); traceFields != nil {
		baseFields = append(baseFields, traceFields...)
	}

	baseFields = append(baseFields, fields...)

	if status == "error" {
		Error("API call failed", baseFields...)
	} else {
		Info("API call", baseFields...)
	}
}

// LogError logs an error with context including trace context
func LogError(ctx context.Context, err error, msg string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.Error(err),
	}

	// Add trace context if available
	if traceFields := extractTraceContext(ctx); traceFields != nil {
		baseFields = append(baseFields, traceFields...)
	}

	baseFields = append(baseFields, fields...)
	Error(msg, baseFields...)
}
