package errortracking

import (
	"fmt"
	"runtime/debug"
	"time"

	posthog "github.com/posthog/posthog-go"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

// zapPostHogLogger bridges the posthog-go SDK logger interface to Zap so that
// SDK-level errors (failed HTTP sends, auth rejections, etc.) appear in the
// structured log pipeline instead of being silently written to os.Stderr.
type zapPostHogLogger struct{}

func (zapPostHogLogger) Debugf(format string, args ...interface{}) {
	logger.Debug(fmt.Sprintf(format, args...))
}

func (zapPostHogLogger) Logf(format string, args ...interface{}) {
	logger.Info(fmt.Sprintf(format, args...))
}

func (zapPostHogLogger) Warnf(format string, args ...interface{}) {
	logger.Warn(fmt.Sprintf(format, args...))
}

func (zapPostHogLogger) Errorf(format string, args ...interface{}) {
	logger.Error(fmt.Sprintf(format, args...))
}

const (
	// distinctID is used for all backend error events since there's no user session.
	distinctID = "backend-service"
)

// client is the package-level singleton.
var client *errorTrackingClient

type errorTrackingClient struct {
	ph posthog.Client
}

// Init initializes the singleton PostHog error tracking client.
// If apiKey is empty, initialization is skipped and a warning is logged.
func Init(apiKey, host, environment, serviceName string, serviceVersion string) {
	if apiKey == "" {
		logger.Warn("PostHog error tracking disabled: POSTHOG_API_KEY not set")
		return
	}

	if host == "" {
		host = "https://eu.i.posthog.com"
	}

	ph, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: host,
		Logger:   zapPostHogLogger{},
		// Attach common properties to every event emitted by this client.
		// These are merged into properties by the SDK before sending.
		DefaultEventProperties: posthog.NewProperties().
			Set("source_system", serviceName).
			Set("environment", environment).
			Set("service_version", serviceVersion),
	})
	if err != nil {
		logger.Warn("Failed to initialize PostHog error tracking client", zap.Error(err))
		return
	}

	client = &errorTrackingClient{ph: ph}

	logger.Info("PostHog error tracking initialized",
		zap.String("host", host),
		zap.String("environment", environment),
	)
}

// Close flushes pending events and shuts down the client. Call this on graceful shutdown.
func Close() {
	if client == nil {
		return
	}
	if err := client.ph.Close(); err != nil {
		logger.Warn("Failed to close PostHog error tracking client", zap.Error(err))
	}
}

// CaptureException reports an error to PostHog error tracking.
// Extra properties are merged into the event (must not contain PII).
// No-ops if the client is not initialized or err is nil.
func CaptureException(err error, properties map[string]interface{}) {
	if client == nil || err == nil {
		return
	}
	errType := fmt.Sprintf("%T", err)

	// Capture the structured stack trace before any further call indirection so
	// the top frame points at the actual error site, not our wrapper.
	// skip=4: runtime.Callers → GetStackTrace → captureWithStack → CaptureException
	extractor := posthog.DefaultStackTraceExtractor{InAppDecider: posthog.SimpleInAppDecider}
	stacktrace := extractor.GetStackTrace(4)

	client.captureWithStack(errType, err.Error(), debug.Stack(), stacktrace, properties)
}

// CapturePanic reports a recovered panic to PostHog error tracking.
// stack must be the output of debug.Stack() captured immediately inside the recover() block
// so it reflects the panic origin, not the recovery site.
// No-ops if the client is not initialized.
func CapturePanic(recovered interface{}, stack []byte) {
	if client == nil {
		return
	}
	panicType := fmt.Sprintf("%T", recovered)
	panicMsg := fmt.Sprintf("%v", recovered)

	// For panics we intentionally skip GetStackTrace: calling it from inside
	// the recovery defer would capture the recovery middleware frames, not where
	// the panic originated. The raw debug.Stack() already has the full origin
	// stack and PostHog surfaces it via $exception_stack_trace_raw.
	client.captureWithStack(panicType, panicMsg, stack, nil, map[string]interface{}{"panic": true})
}

// captureWithStack sends the exception to PostHog using posthog.Capture with event="$exception".
//
// Why Capture and not posthog.Exception?
//
// The SDK provides posthog.Exception as a dedicated error type, but it has two
// hard limitations discovered by reading the SDK source (posthog.go lines 400-423):
//
//  1. DefaultEventProperties is NOT merged into Exception events — only Capture
//     events get that treatment. So source_system/environment can't be injected
//     via the client config.
//
//  2. ExceptionInApiProperties is a fixed struct with no free-form Properties map,
//     so custom properties (source_system, environment, service_version, http_path,
//     panic=true, etc.) cannot be attached to an Exception event at all.
//
// posthog.Capture with event="$exception" and a properly structured $exception_list
// property is processed by PostHog's Error Tracking pipeline identically — PostHog
// identifies exception events by event name, not by the SDK message type.
// DefaultEventProperties IS merged into Capture events, which is how source_system
// and environment reach every error event automatically.
func (c *errorTrackingClient) captureWithStack(
	errType, errMsg string,
	rawStack []byte,
	structuredStack *posthog.ExceptionStacktrace,
	extraProps map[string]interface{},
) {

	props := posthog.NewProperties().
		Set("$exception_type", errType).
		Set("$exception_message", errMsg).
		Set("$exception_stack_trace_raw", string(rawStack)).
		Set("$exception_list", []posthog.ExceptionItem{
			{
				Type:       errType,
				Value:      errMsg,
				Stacktrace: structuredStack, // nil for panics; omitted via omitempty
			},
		})

	for k, v := range extraProps {
		props.Set(k, v)
	}

	if err := c.ph.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      "$exception",
		Timestamp:  time.Now().UTC(),
		Properties: props,
	}); err != nil {
		logger.Warn("Failed to enqueue PostHog exception event",
			zap.String("error_type", errType),
			zap.Error(err),
		)
	}
}
