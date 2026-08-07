package logger

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// NewRedactingCore wraps a core so every string that reaches an encoder has been
// through pkg/redact first. It is installed by Initialize on both construction
// paths, so it covers every binary and every logger derived from the global one
// — including the children handed out by With, which is how the worker's cron
// logger is built.
//
// # WHY A CORE AND NOT MORE CALL-SITE DISCIPLINE
//
// Call-site discipline is still the primary rule — RedactedError exists, and the
// explicit masked fields next to it say what a line is allowed to carry. But
// C11's finding was a line that had been reviewed twice and still shipped a real
// address to Loki on every single email the platform sent, and the audit found
// 74 remaining zap.Error sites, each of which is one wrapped repository error
// away from quoting a row's email or a mentor's name. Auditing 74 sites forever
// is not a control; a choke point every field must pass is.
//
// zap.Hooks is not an option here: a hook receives only the Entry, never the
// fields, so it would have covered none of the sites that actually leaked.
//
// WHAT IT DOES NOT COVER, so that nobody mistakes it for total:
//   - Values that are PII by meaning rather than by shape. A person's name is
//     just a string; no rule can find it. Names are dropped at the call site.
//   - Marshaled objects (zapcore.ObjectMarshaler, arrays of them). Nothing in
//     this codebase logs one; if that changes, the field bypasses this.
//   - internal/handlers/logs_handler.go, which writes browser-supplied lines to
//     frontend.log with encoding/json and never touches zap. It redacts itself.
func NewRedactingCore(inner zapcore.Core) zapcore.Core {
	return &redactingCore{Core: inner}
}

type redactingCore struct {
	zapcore.Core
}

// With redacts the bound fields once, at bind time, rather than on every write.
func (c *redactingCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactingCore{Core: c.Core.With(redactFields(fields))}
}

// Check must name THIS core, not the wrapped one: the CheckedEntry records which
// core to call Write on, and handing it the inner core would route every entry
// straight past the redaction.
func (c *redactingCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

func (c *redactingCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// The message is a sink too. In this codebase messages are constants (which
	// is what keeps Loki grouping usable) and redact.Text is a no-op on them, but
	// pkg/errortracking funnels the PostHog SDK's own formatted text through
	// logger.Warn as a message, and that text is not ours.
	entry.Message = redact.Text(entry.Message)
	return c.Core.Write(entry, redactFields(fields))
}

// redactFields copies lazily: log lines whose fields need no change — the common
// case, since ids are already hashed at the call site — keep the caller's slice
// and allocate nothing. Copying at all matters because the slice can be built
// once and reused across calls (pkg/trigger holds its url field), so redacting
// in place would rewrite a caller's own data.
func redactFields(fields []zapcore.Field) []zapcore.Field {
	out := fields
	copied := false
	for i := range fields {
		replacement, changed := redactField(fields[i])
		if !changed {
			continue
		}
		if !copied {
			out = make([]zapcore.Field, len(fields))
			copy(out, fields)
			copied = true
		}
		out[i] = replacement
	}
	return out
}

func redactField(field zapcore.Field) (zapcore.Field, bool) {
	switch field.Type {
	case zapcore.StringType:
		value := redactValue(field.Key, field.String)
		if value == field.String {
			return field, false
		}
		field.String = value
		return field, true

	case zapcore.ErrorType:
		// zap.Error(nil) is a Skip, so an ErrorType always holds a non-nil error.
		// Rewriting it as a string also drops zap's errorVerbose, which is the
		// same value again with more of the wrapping chain in it — no loss worth
		// keeping and one less place for a DSN or an address to sit.
		err, ok := field.Interface.(error)
		if !ok {
			return field, false
		}
		return zap.String(field.Key, redactValue(field.Key, err.Error())), true

	case zapcore.StringerType:
		stringer, ok := field.Interface.(fmt.Stringer)
		if !ok {
			return field, false
		}
		return zap.String(field.Key, redactValue(field.Key, stringer.String())), true

	case zapcore.ReflectType:
		// zap.Any routes errors and Stringers to their own types, so what lands
		// here is a plain value. The shape this codebase actually logs is the
		// middleware's route/query param maps; a recovered panic value that is
		// neither error nor Stringer has no string to reach into.
		if values, ok := field.Interface.(map[string]string); ok {
			return zap.Any(field.Key, redactStringMap(values)), true
		}
		return field, false
	}

	// Everything else — numbers, bools, durations, and the array marshalers
	// zap.Strings produces — is either not a string sink or not reachable: an
	// ArrayMarshaler has already been closed over by the time it gets here, so
	// zap.Strings must be redacted at its call site.
	return field, false
}

// redactValue is the single rule for "what may a log field's string value be".
// A sensitive key loses its value outright — knowing the field was present is
// the useful part — and everything else goes through the shape rules, which is
// what catches an address or a DSN inside a value whose key says nothing.
func redactValue(key, value string) string {
	if value == "" {
		return value
	}
	if redact.SensitiveKey(key) {
		return redact.Placeholder
	}
	return redact.Text(value)
}

func redactStringMap(values map[string]string) map[string]string {
	redacted := make(map[string]string, len(values))
	for key, value := range values {
		redacted[key] = redactValue(key, value)
	}
	return redacted
}

// RedactedStrings is for the one field shape the core cannot reach: zap.Strings
// hands the encoder a closed-over ArrayMarshaler, so a slice of user-supplied
// values has to be redacted before it becomes a field.
func RedactedStrings(key string, values []string) zap.Field {
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = redactValue(key, value)
	}
	return zap.Strings(key, redacted)
}
