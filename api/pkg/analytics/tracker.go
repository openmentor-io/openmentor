package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	// EU, matching config.go's POSTHOG_HOST default and errortracking's. This is
	// a European product whose PostHog project lives in the EU region, and a
	// package-level default that pointed at us.i.posthog.com was a
	// GDPR-relevant divergence waiting for the one caller that constructs a
	// Config without a host — a cross-region transfer nobody would have reviewed.
	// (It was never reached in production: viper's POSTHOG_HOST default fills
	// cfg.PostHog.Host even when the env var is set to the empty string.)
	DefaultPostHogHost       = "https://eu.i.posthog.com"
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

// Flusher is implemented by trackers that buffer events off the request path and
// therefore have something to lose at shutdown. It is deliberately NOT part of
// Tracker: every service and its test doubles depend on Tracker, and only the
// two binaries that own a tracker's lifetime should be able to end it.
type Flusher interface {
	Close(ctx context.Context) error
}

// Close drains tracker's buffered events, bounded by ctx, and is a no-op for a
// tracker that buffers nothing. Call it once, after the thing producing events
// has stopped.
func Close(ctx context.Context, tracker Tracker) error {
	flusher, ok := tracker.(Flusher)
	if !ok {
		return nil
	}
	return flusher.Close(ctx)
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

// Close satisfies Flusher so the binaries do not have to care which tracker they
// were handed. Nothing is buffered, so there is nothing to drain.
func (NoopTracker) Close(context.Context) error { return nil }

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

	// closeMu guards closed against Track, so Close can close the queue knowing
	// no Track is about to send on it. Track's send is non-blocking, so the read
	// lock is never held for longer than a channel write.
	closeMu sync.RWMutex
	closed  bool
	// drained is closed by the worker goroutine once it has emptied the queue.
	drained chan struct{}
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
		drained:             make(chan struct{}),
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

	t.closeMu.RLock()
	defer t.closeMu.RUnlock()
	if t.closed {
		// Sending here would panic on a closed channel. Events raised after the
		// drain began are lost by definition; say so rather than crash the
		// shutdown that is already under way.
		logger.Warn("analytics tracker is closed; dropping event",
			zap.String("provider", string(t.provider)),
			zap.String("event", event))
		return
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
	defer close(t.drained)
	for event := range t.queue {
		t.sendPostHog(event)
	}
}

// Close stops accepting events and waits for the queued ones to be sent, giving
// up when ctx expires.
//
// Without it a deploy silently discarded whatever was still queued — up to
// defaultQueueSize (512) events, and in practice every event raised in the last
// moments before SIGTERM, which is exactly the window a bad deploy's telemetry
// lands in.
//
// The wait is bounded by ctx and by nothing else. The single sender goroutine is
// mid-HTTP-request at worst, and that request already carries the client's own
// timeout, so a hung PostHog can delay this by defaultTimeout and not longer.
// After ctx expires the goroutine is simply abandoned: the process is exiting,
// and blocking shutdown to finish telemetry would be the wrong trade.
func (t *AnalyticsTracker) Close(ctx context.Context) error {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return nil
	}
	t.closed = true
	queued := len(t.queue)
	close(t.queue)
	t.closeMu.Unlock()

	select {
	case <-t.drained:
		logger.Info("Analytics queue drained",
			zap.String("provider", string(t.provider)),
			zap.Int("queued_events", queued))
		return nil
	case <-ctx.Done():
		logger.Warn("Analytics drain deadline reached; queued events were dropped",
			zap.String("provider", string(t.provider)),
			zap.Int("queued_events", queued),
			zap.Int("undrained_events", len(t.queue)))
		return fmt.Errorf("analytics drain: %w", ctx.Err())
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

func ReviewDistinctID(reviewID string) string {
	return prefixedDistinctID("review", reviewID)
}

// AnonymousDistinctID is the distinct id for events that must not create a
// person record. Track substitutes the source system's own id, so events group
// under "system:api" / "system:worker" instead of the capability-bearing
// "request:<uuid>" person the review and contact flows used to create (P14).
func AnonymousDistinctID() string {
	return ""
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
		if normalizedKey == "" || value == nil || isBlockedProperty(normalizedKey, value) {
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

// blockedPropertyKeys are matched against the SEPARATOR-FREE spelling of a
// property key, so request_id and requestId are one entry (mirrors
// blockedPropertyKeys in web/src/lib/analytics.ts).
var blockedPropertyKeys = map[string]struct{}{
	"email":          {},
	"mentoremail":    {},
	"moderatoremail": {},
	"name":           {},
	"mentorname":     {},
	"moderatorname":  {},
	"contact":        {},
	"intro":          {},
	"description":    {},
	"review":         {},
	"mentorreview":   {},
	"platformreview": {},
	"improvements":   {},
	"loginurl":       {},
	"confirmurl":     {},
}

// blockedPropertyKeySuffixes catch credential- and capability-shaped keys
// without enumerating every spelling: request_id / client_request_id are bearer
// tokens for the review flow (P14), and login_token / confirm_token / captchaToken
// are credentials.
var blockedPropertyKeySuffixes = []string{
	"requestid",
	"token",
	"secret",
	"password",
}

func isBlockedProperty(key string, value interface{}) bool {
	normalized := normalizePropertyKey(key)
	if normalized == "" {
		return false
	}

	if _, found := blockedPropertyKeys[normalized]; found {
		return true
	}

	// A capability or credential is always a string, so a shape-matched key
	// carrying a boolean or a number is a derived fact worth keeping
	// (has_request_id, captcha_token_length).
	switch value.(type) {
	case bool, int, int8, int16, int32, int64, float32, float64, uint, uint8, uint16, uint32, uint64:
		return false
	}

	for _, suffix := range blockedPropertyKeySuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}

	return false
}

// normalizePropertyKey folds case and separators so request_id, requestId and
// REQUEST-ID compare equal.
func normalizePropertyKey(key string) string {
	var builder strings.Builder
	builder.Grow(len(key))
	for _, r := range strings.ToLower(strings.TrimSpace(key)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
