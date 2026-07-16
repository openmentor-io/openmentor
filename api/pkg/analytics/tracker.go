package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	DefaultPostHogHost       = "https://us.i.posthog.com"
	DefaultEventVersion      = "v1"
	defaultTimeout           = 3 * time.Second
	defaultQueueSize         = 512
	defaultSource            = "api"
	defaultEnvironment       = "unknown"
	defaultAnalyticsProvider = ProviderNone
	providerNoneValue        = "none"
	providerPostHogValue     = "posthog"
)

type Tracker interface {
	Track(ctx context.Context, event string, distinctID string, properties map[string]interface{})
}

type Provider string

const (
	ProviderNone    Provider = providerNoneValue
	ProviderPostHog Provider = providerPostHogValue
)

type Config struct {
	Provider     string
	SourceSystem string
	Environment  string
	EventVersion string
	Timeout      time.Duration
	QueueSize    int
	HTTPClient   *http.Client

	// PostHog
	PostHogEnabled         bool
	PostHogAPIKey          string
	PostHogHost            string
	PostHogCaptureEndpoint string
	PostHogDisableGeoIP    bool
}

type NoopTracker struct{}

func (NoopTracker) Track(context.Context, string, string, map[string]interface{}) {}

type AnalyticsTracker struct {
	provider            Provider
	posthogAPIKey       string
	posthogEndpoint     string
	posthogDisableGeoIP bool
	sourceSystem        string
	environment         string
	eventVersion        string
	httpClient          *http.Client
	queue               chan queuedEvent
}

type queuedEvent struct {
	event      string
	distinctID string
	properties map[string]interface{}
	occurredAt time.Time
}

type posthogPayload struct {
	APIKey       string                 `json:"api_key"`
	Event        string                 `json:"event"`
	DistinctID   string                 `json:"distinct_id"`
	Properties   map[string]interface{} `json:"properties"`
	Timestamp    string                 `json:"timestamp"`
	DisableGeoIP bool                   `json:"disable_geoip,omitempty"`
}

func NewTracker(cfg *Config) Tracker {
	if cfg == nil {
		return NoopTracker{}
	}

	posthogAPIKey := strings.TrimSpace(cfg.PostHogAPIKey)
	posthogEndpoint := normalizePostHogEndpoint(cfg.PostHogHost, cfg.PostHogCaptureEndpoint)

	resolvedProvider := resolveProvider(
		cfg.Provider,
		cfg.PostHogEnabled,
		posthogAPIKey != "" && posthogEndpoint != "",
	)
	if resolvedProvider == ProviderNone {
		return NoopTracker{}
	}

	sourceSystem := strings.TrimSpace(cfg.SourceSystem)
	if sourceSystem == "" {
		sourceSystem = defaultSource
	}

	environment := strings.TrimSpace(cfg.Environment)
	if environment == "" {
		environment = defaultEnvironment
	}

	eventVersion := strings.TrimSpace(cfg.EventVersion)
	if eventVersion == "" {
		eventVersion = DefaultEventVersion
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}

	tracker := &AnalyticsTracker{
		provider:            resolvedProvider,
		posthogAPIKey:       posthogAPIKey,
		posthogEndpoint:     posthogEndpoint,
		posthogDisableGeoIP: cfg.PostHogDisableGeoIP,
		sourceSystem:        sourceSystem,
		environment:         environment,
		eventVersion:        eventVersion,
		httpClient:          httpClient,
		queue:               make(chan queuedEvent, queueSize),
	}
	go tracker.runWorker()

	return tracker
}

func (t *AnalyticsTracker) Track(ctx context.Context, event string, distinctID string, properties map[string]interface{}) {
	event = strings.TrimSpace(event)
	if event == "" {
		return
	}

	cleanDistinctID := strings.TrimSpace(distinctID)
	if cleanDistinctID == "" {
		cleanDistinctID = SystemDistinctID(t.sourceSystem)
	}

	cleanProperties := sanitizeProperties(properties)
	cleanProperties["source_system"] = t.sourceSystem
	cleanProperties["environment"] = t.environment
	cleanProperties["event_version"] = t.eventVersion

	// Correlate the analytics event with the active trace so PostHog events
	// can be joined against backend traces.
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		cleanProperties["trace_id"] = spanCtx.TraceID().String()
	}

	item := queuedEvent{
		event:      event,
		distinctID: cleanDistinctID,
		properties: cleanProperties,
		occurredAt: time.Now().UTC(),
	}

	select {
	case t.queue <- item:
	default:
		logger.Warn("analytics queue is full; dropping event",
			zap.String("provider", string(t.provider)),
			zap.String("event", event),
			zap.Int("queue_capacity", cap(t.queue)))
	}
}

func (t *AnalyticsTracker) runWorker() {
	for event := range t.queue {
		t.sendPostHog(event)
	}
}

func (t *AnalyticsTracker) sendPostHog(event queuedEvent) {
	if t.posthogAPIKey == "" || t.posthogEndpoint == "" {
		return
	}

	posthogProps := cloneProperties(event.properties)
	posthogProps["distinct_id"] = event.distinctID

	payload := posthogPayload{
		APIKey:       t.posthogAPIKey,
		Event:        event.event,
		DistinctID:   event.distinctID,
		Properties:   posthogProps,
		Timestamp:    event.occurredAt.Format(time.RFC3339Nano),
		DisableGeoIP: t.posthogDisableGeoIP,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("Failed to marshal PostHog event payload",
			zap.String("event", event.event),
			zap.Error(err))
		return
	}

	t.postJSON(t.posthogEndpoint, event.event, body)
}

func (t *AnalyticsTracker) postJSON(endpoint string, eventName string, body []byte) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		logger.Warn("Failed to create analytics request",
			zap.String("event", eventName),
			zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		logger.Warn("Failed to send analytics event",
			zap.String("event", eventName),
			zap.String("endpoint", endpoint),
			zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		bodyPreview, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if readErr != nil {
			logger.Warn("Analytics provider returned non-success status and response body could not be read",
				zap.String("event", eventName),
				zap.String("endpoint", endpoint),
				zap.Int("status_code", resp.StatusCode),
				zap.Error(readErr))
			return
		}
		logger.Warn("Analytics provider returned non-success status",
			zap.String("event", eventName),
			zap.String("endpoint", endpoint),
			zap.Int("status_code", resp.StatusCode),
			zap.String("response", string(bodyPreview)))
	}
}

func resolveProvider(requestedProvider string, posthogEnabled, posthogReady bool) Provider {
	switch normalizeProvider(requestedProvider) {
	case "":
		if posthogEnabled && posthogReady {
			return ProviderPostHog
		}
		return defaultAnalyticsProvider
	case providerNoneValue:
		return ProviderNone
	case providerPostHogValue:
		if posthogReady {
			return ProviderPostHog
		}
		logger.Warn("Analytics provider posthog requested but not configured")
		return ProviderNone
	default:
		logger.Warn("Unsupported analytics provider requested", zap.String("provider", requestedProvider))
		return ProviderNone
	}
}

func normalizeProvider(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func normalizePostHogEndpoint(host, endpoint string) string {
	override := strings.TrimSpace(endpoint)
	if override != "" {
		return override
	}

	cleanHost := strings.TrimSpace(host)
	if cleanHost == "" {
		cleanHost = DefaultPostHogHost
	}
	return strings.TrimRight(cleanHost, "/") + "/capture/"
}

func cloneProperties(properties map[string]interface{}) map[string]interface{} {
	if len(properties) == 0 {
		return map[string]interface{}{}
	}
	clone := make(map[string]interface{}, len(properties))
	for key, value := range properties {
		clone[key] = value
	}
	return clone
}

func MentorDistinctID(mentorID string) string {
	return prefixedDistinctID("mentor", mentorID)
}

func ModeratorDistinctID(moderatorID string) string {
	return prefixedDistinctID("moderator", moderatorID)
}

func RequestDistinctID(requestID string) string {
	return prefixedDistinctID("request", requestID)
}

func ReviewDistinctID(reviewID string) string {
	return prefixedDistinctID("review", reviewID)
}

func SystemDistinctID(system string) string {
	cleanSystem := strings.TrimSpace(system)
	if cleanSystem == "" {
		cleanSystem = defaultSource
	}
	return fmt.Sprintf("system:%s", cleanSystem)
}

func prefixedDistinctID(prefix, id string) string {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", prefix, cleanID)
}

func sanitizeProperties(properties map[string]interface{}) map[string]interface{} {
	if len(properties) == 0 {
		return map[string]interface{}{}
	}

	safe := make(map[string]interface{}, len(properties))
	for key, value := range properties {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" || isBlockedPropertyKey(normalizedKey) || value == nil {
			continue
		}

		switch typedValue := value.(type) {
		case string:
			safe[normalizedKey] = trimStringValue(typedValue)
		case bool, int, int8, int16, int32, int64, float32, float64, uint, uint8, uint16, uint32, uint64:
			safe[normalizedKey] = typedValue
		case time.Time:
			safe[normalizedKey] = typedValue.Unix()
		case []string:
			safe[normalizedKey] = typedValue
		default:
			safe[normalizedKey] = trimStringValue(fmt.Sprint(typedValue))
		}
	}

	return safe
}

func trimStringValue(input string) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) <= 512 {
		return trimmed
	}
	return trimmed[:512]
}

func isBlockedPropertyKey(key string) bool {
	blockedKeys := map[string]struct{}{
		"email":           {},
		"mentor_email":    {},
		"moderator_email": {},
		"name":            {},
		"mentor_name":     {},
		"moderator_name":  {},
		"contact":         {},
		"intro":           {},
		"description":     {},
		"review":          {},
		"mentor_review":   {},
		"platform_review": {},
		"improvements":    {},
		"login_url":       {},
	}

	_, found := blockedKeys[strings.ToLower(strings.TrimSpace(key))]
	return found
}
