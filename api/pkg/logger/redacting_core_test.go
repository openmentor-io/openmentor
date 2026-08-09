package logger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// menteeAddress is an RFC 2606 example.com address, so nothing in this file or in
// a CI failure message can ever be a real person's.
const menteeAddress = "mentee@example.com"

// stringerError is both an error and a Stringer, which is how AWS and pgx errors
// reach a log field.
type stringerAddress struct{ address string }

func (s stringerAddress) String() string { return "destination " + s.address }

func observedRedactingLogger(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()

	core, logs := observer.New(zap.DebugLevel)
	// The SAME wrapper Initialize installs. Wrapping here rather than asserting on
	// redactField directly is the point: it proves the entry actually travels
	// through the redaction on its way to an encoder.
	return zap.New(NewRedactingCore(core)), logs
}

func rendered(t *testing.T, logs *observer.ObservedLogs) string {
	t.Helper()

	var out strings.Builder
	for _, entry := range logs.All() {
		out.WriteString(entry.Message)
		encoded, err := json.Marshal(entry.ContextMap())
		if err != nil {
			t.Fatalf("marshal log context: %v", err)
		}
		out.Write(encoded)
	}
	return out.String()
}

// TestRedactingCoreCatchesEveryFieldShape is the defense-in-depth claim under
// test: a raw address reaches Loki through whichever field shape the call site
// happened to use, so all of them have to be covered.
func TestRedactingCoreCatchesEveryFieldShape(t *testing.T) {
	shapes := []struct {
		name  string
		field zap.Field
	}{
		{"zap.String", zap.String("recipient", menteeAddress)},
		{"zap.Error", zap.Error(errors.New("rejected identity " + menteeAddress))},
		{"zap.NamedError", zap.NamedError("cause", errors.New("rejected identity "+menteeAddress))},
		{"zap.Stringer", zap.Stringer("target", stringerAddress{menteeAddress})},
		{"zap.Any(error)", zap.Any("panic", errors.New("panic on "+menteeAddress))},
		{"zap.Any(string)", zap.Any("panic", "panic on "+menteeAddress)},
		{"zap.Any(map)", zap.Any("query_params", map[string]string{"email": menteeAddress})},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			log, logs := observedRedactingLogger(t)
			log.Info("send failed", shape.field)

			got := rendered(t, logs)
			if strings.Contains(got, menteeAddress) {
				t.Errorf("%s reached the encoder with the address intact: %s", shape.name, got)
			}
			if !strings.Contains(got, "m***@example.com") {
				t.Errorf("%s lost the masked form, so the line is no longer debuggable: %s", shape.name, got)
			}
		})
	}
}

// TestRedactingCoreRedactsTheMessage covers pkg/errortracking, which funnels the
// PostHog SDK's own formatted text through Warn as a message rather than a field.
func TestRedactingCoreRedactsTheMessage(t *testing.T) {
	log, logs := observedRedactingLogger(t)
	log.Warn("posthog rejected the event for " + menteeAddress)

	if got := rendered(t, logs); strings.Contains(got, menteeAddress) {
		t.Errorf("the message reached the encoder with the address intact: %s", got)
	}
}

// TestRedactingCoreCoversChildLoggers matters because With() hands out a raw
// *zap.Logger that the worker's cron loop keeps for the life of the process.
func TestRedactingCoreCoversChildLoggers(t *testing.T) {
	log, logs := observedRedactingLogger(t)

	child := log.With(zap.String("recipient", menteeAddress))
	child.Info("bound at construction")
	child.With(zap.String("cc", menteeAddress)).Info("bound twice")

	if got := rendered(t, logs); strings.Contains(got, menteeAddress) {
		t.Errorf("a child logger's bound field kept the address: %s", got)
	}
}

func TestRedactingCoreDropsSensitiveKeysEntirely(t *testing.T) {
	log, logs := observedRedactingLogger(t)
	log.Info("login", zap.String("login_token", "abc123"), zap.String("status", "ok"))

	got := rendered(t, logs)
	if strings.Contains(got, "abc123") {
		t.Errorf("a sensitive key kept its value: %s", got)
	}
	if !strings.Contains(got, redact.Placeholder) {
		t.Errorf("the field was dropped rather than blanked, so the line no longer says it was present: %s", got)
	}
	if !strings.Contains(got, "ok") {
		t.Errorf("an innocuous field was collateral damage: %s", got)
	}
}

// TestRedactingCoreLeavesUsefulFieldsAlone is the other half of the trade. A
// redaction layer that mangles ids, slugs and statuses would be turned off within
// a week.
func TestRedactingCoreLeavesUsefulFieldsAlone(t *testing.T) {
	log, logs := observedRedactingLogger(t)
	log.Info("HTTP request",
		zap.String("mentor_id", "11111111-2222-4333-8444-555555555555"),
		zap.String("slug", "alice-mentor"),
		zap.String("status", "active"),
		zap.String("template", "new-review"),
		zap.Int("http_status", 200),
		zap.String("trace_id", "0af7651916cd43dd8448eb211c80319c"),
	)

	entry := logs.All()[0].ContextMap()
	for key, want := range map[string]interface{}{
		"slug":        "alice-mentor",
		"status":      "active",
		"template":    "new-review",
		"http_status": int64(200),
		"trace_id":    "0af7651916cd43dd8448eb211c80319c",
	} {
		if entry[key] != want {
			t.Errorf("%s = %v, want %v — the core over-redacted a field an operator needs", key, entry[key], want)
		}
	}
	// mentor_id is a UUID, and every other sink in the codebase already hashes
	// those (P14), so the core doing the same is consistent, not surprising.
	if entry["mentor_id"] == "11111111-2222-4333-8444-555555555555" {
		t.Error("mentor_id was not hashed, so the core disagrees with redact.Path")
	}
	if !strings.HasPrefix(entry["mentor_id"].(string), "sha256:") {
		t.Errorf("mentor_id = %v, want a stable hashed reference", entry["mentor_id"])
	}
}

// TestInitializeInstallsTheRedactingCore is the test that would have caught C11
// in the first place: it drives the real Initialize, the real JSON encoder and
// the real on-disk app.log that Alloy tails.
func TestInitializeInstallsTheRedactingCore(t *testing.T) {
	logDir := t.TempDir()

	previous := Log
	t.Cleanup(func() { Log = previous })

	if err := Initialize(Config{
		Level:       "debug",
		LogDir:      logDir,
		Environment: "production",
		ServiceName: "openmentor-test",
	}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	Error("SES email send failed",
		zap.String("recipient", menteeAddress),
		zap.Error(errors.New("rejected identity "+menteeAddress)),
	)
	Sync()

	for _, name := range []string{"app.log", "error.log"} {
		contents, err := os.ReadFile(filepath.Join(logDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(contents) == 0 {
			t.Fatalf("%s is empty: the test wrote nothing and proves nothing", name)
		}
		if strings.Contains(string(contents), menteeAddress) {
			t.Errorf("%s on disk carries the raw address: %s", name, contents)
		}
		if !strings.Contains(string(contents), "m***@example.com") {
			t.Errorf("%s lost the masked recipient: %s", name, contents)
		}
	}
}

// TestErrorLogStillReceivesOnlyErrors pins the per-sink level routing the
// redaction wrapping must not break: routing lives in each child core's Check
// (Write on an ioCore is unconditional), so wrapping the TEE — rather than each
// child — would register the wrapper once and fan every info line out to
// error.log, turning it into a duplicate of app.log that rotates real errors
// away at full log rate.
func TestErrorLogStillReceivesOnlyErrors(t *testing.T) {
	logDir := t.TempDir()

	previous := Log
	t.Cleanup(func() { Log = previous })

	if err := Initialize(Config{
		Level:       "debug",
		LogDir:      logDir,
		Environment: "production",
		ServiceName: "openmentor-test",
	}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	Info("routine line that belongs in app.log only")
	Error("genuine error line")
	Sync()

	appLog, err := os.ReadFile(filepath.Join(logDir, "app.log"))
	if err != nil {
		t.Fatalf("read app.log: %v", err)
	}
	if !strings.Contains(string(appLog), "routine line") || !strings.Contains(string(appLog), "genuine error line") {
		t.Fatalf("app.log must carry both lines: %s", appLog)
	}

	errorLog, err := os.ReadFile(filepath.Join(logDir, "error.log"))
	if err != nil {
		t.Fatalf("read error.log: %v", err)
	}
	if strings.Contains(string(errorLog), "routine line") {
		t.Errorf("error.log received an info line — the redacting wrapper broke level routing: %s", errorLog)
	}
	if !strings.Contains(string(errorLog), "genuine error line") {
		t.Errorf("error.log lost the error line: %s", errorLog)
	}
}

// panickyStringer reproduces what zap's encoder tolerates (encodeStringer
// recovers): a String() that panics must not take the Write path down now that
// the redacting core calls it earlier, where nothing else recovers.
type panickyStringer struct{}

func (panickyStringer) String() string { panic("boom") }

func TestRedactingCoreSurvivesAPanickingStringer(t *testing.T) {
	log, logs := observedRedactingLogger(t)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("logging a panicking Stringer panicked the caller: %v", recovered)
		}
	}()
	log.Info("stringer field", zap.Stringer("value", panickyStringer{}))

	if logs.Len() != 1 {
		t.Fatalf("the log line was dropped entirely: %d entries", logs.Len())
	}
}

// TestInitializeInstallsTheRedactingCoreOnTheNonProductionPath pins the OTHER
// construction branch, which builds through zap.Config and was the one an earlier
// pass would have been easiest to forget.
func TestInitializeInstallsTheRedactingCoreOnTheNonProductionPath(t *testing.T) {
	previous := Log
	t.Cleanup(func() { Log = previous })

	if err := Initialize(Config{Level: "debug", ServiceName: "openmentor-test"}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// The built logger writes to stdout, so assert on the core rather than on the
	// bytes: Check must route the entry through the redacting wrapper.
	if _, ok := Log.Core().(*redactingCore); !ok {
		t.Fatalf("core = %T, want the redacting wrapper", Log.Core())
	}
}

// TestRedactedStringsCoversTheShapeTheCoreCannot documents the one known hole and
// pins its workaround, so nobody "simplifies" it back to zap.Strings.
func TestRedactedStringsCoversTheShapeTheCoreCannot(t *testing.T) {
	log, logs := observedRedactingLogger(t)

	log.Info("bypasses the core", zap.Strings("unresolved_tags", []string{menteeAddress}))
	if got := rendered(t, logs); !strings.Contains(got, menteeAddress) {
		t.Skip("zap.Strings is now reachable from a core; RedactedStrings can be reconsidered")
	}

	log, logs = observedRedactingLogger(t)
	log.Info("covered", RedactedStrings("unresolved_tags", []string{menteeAddress}))
	if got := rendered(t, logs); strings.Contains(got, menteeAddress) {
		t.Errorf("RedactedStrings kept the address: %s", got)
	}
}

var _ zapcore.Core = (*redactingCore)(nil)
