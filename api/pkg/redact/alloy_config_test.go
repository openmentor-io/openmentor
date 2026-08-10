package redact

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// alloyConfig is the live Alloy pipeline's config. Reading it from a Go test is
// deliberate: infra/alloy/config.alloy holds the last-resort redaction stage, and
// Alloy compiles those expressions with Go's own RE2 — this package's regexp
// engine IS that engine, which no bash or Python check can claim.
//
// infra/alloy-redaction-test.sh owns the behavior cases (and always runs when
// infra changes). This test owns the one thing it cannot do: prove the
// expressions compile in the engine that will run them. An expression Alloy
// cannot compile does not fail `alloy validate` — the stage builds it at runtime
// — so without this there is nothing between a bad regex and a broken pipeline.
const alloyConfig = "../../../infra/alloy/config.alloy"

func TestAlloyRedactionExpressionsCompileAsRE2(t *testing.T) {
	expressions := alloyRedactionExpressions(t)
	if len(expressions) < 3 {
		t.Fatalf("found %d expressions in %s, want at least 3 (dsn, credentials, email)", len(expressions), alloyConfig)
	}

	for i, expression := range expressions {
		compiled, err := regexp.Compile(expression)
		if err != nil {
			t.Errorf("expression %d does not compile as RE2, so the stage would fail at runtime: %v\n  %s", i+1, err, expression)
			continue
		}
		// stage.replace substitutes the capture groups, not the whole match. An
		// expression with no group would replace the entire match and take the
		// surrounding JSON with it.
		if compiled.NumSubexp() == 0 {
			t.Errorf("expression %d has no capture group: stage.replace would eat the whole match\n  %s", i+1, expression)
		}
	}
}

// TestAlloyEmailRuleAgreesWithTheGoMasker is the seam between the two layers. The
// Alloy stage exists to catch what the Go layer misses, so it has to fire on
// exactly the addresses Email masks — and, just as importantly, NOT fire on the
// output Email produces, or every correctly-masked line would be mangled a second
// time on its way out.
func TestAlloyEmailRuleAgreesWithTheGoMasker(t *testing.T) {
	rule := regexp.MustCompile(alloyEmailExpression(t))

	shouldFire := []string{
		"mentee.person+tag@example.com",
		"a@b.co",
		"first.last@sub.example.co.uk",
	}
	for _, address := range shouldFire {
		if !rule.MatchString(address) {
			t.Errorf("the Alloy net does not catch %q, which Email masks — the last resort has a hole", address)
		}
	}

	shouldNotFire := []string{
		// The Go layer's own output.
		Email("mentee.person@example.com"),
		Email("a@example.com"),
		EmailMask,
		// The false positives both layers must agree to leave alone.
		"github.com/openmentor-io/openmentor@v1.2.3",
		"grafana/alloy@sha256:deadbeefdeadbeef",
	}
	for _, value := range shouldNotFire {
		if rule.MatchString(value) {
			t.Errorf("the Alloy net fires on %q, which the Go layer already handled or must not touch", value)
		}
	}
}

// TestAlloySecretRulesLeaveTheGoMasksAlone pins the marker's meaning: a
// [REDACTED_SECRET] in Loki must indicate a value the application layer MISSED.
// The DSN and credential stages therefore must not re-match the Go layer's own
// correct outputs — redact.DSN's `user:***@host`, the core's `[REDACTED]`
// placeholder — or their own [REDACTED_SECRET], while still firing on the raw
// values those masks replace.
func TestAlloySecretRulesLeaveTheGoMasksAlone(t *testing.T) {
	expressions := alloyRedactionExpressions(t)
	if len(expressions) < 3 {
		t.Fatalf("want at least 3 expressions (dsn, credentials, email), got %d", len(expressions))
	}
	dsnRule := regexp.MustCompile(expressions[0])
	credentialRule := regexp.MustCompile(expressions[1])

	if !dsnRule.MatchString(`postgres://om:realpassword@db.internal:5432/om`) {
		t.Error("the DSN rule does not catch a raw password — the last resort has a hole")
	}
	for _, masked := range []string{
		DSN("postgres://om:realpassword@db.internal:5432/om"),
		`postgres://om:[REDACTED_SECRET]@db.internal:5432/om`,
	} {
		if dsnRule.MatchString(masked) {
			t.Errorf("the DSN rule fires on %q, which is already masked — [REDACTED_SECRET] would stop meaning \"the app layer failed\"", masked)
		}
	}

	if !credentialRule.MatchString(`"login_token":"mtk_abcdef123456"`) {
		t.Error("the credential rule does not catch a raw token — the last resort has a hole")
	}
	for _, masked := range []string{
		`"login_token":"` + Placeholder + `"`,
		`"password":"***"`,
		`"api_key":"[REDACTED_SECRET]"`,
	} {
		if credentialRule.MatchString(masked) {
			t.Errorf("the credential rule fires on %q, which is already masked — [REDACTED_SECRET] would stop meaning \"the app layer failed\"", masked)
		}
	}
}

// alloyRedactionExpressions lifts the stage.replace expressions out of the
// redact_pii component, unescaping river's doubled backslashes.
func alloyRedactionExpressions(t *testing.T) []string {
	t.Helper()

	contents, err := os.ReadFile(alloyConfig)
	if err != nil {
		t.Fatalf("read %s: %v — this test is the only RE2 check on the live pipeline's regexes", alloyConfig, err)
	}

	lines := strings.Split(string(contents), "\n")
	expressions := make([]string, 0, 4)
	inComponent := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, `loki.process "redact_pii" {`):
			inComponent = true
		case inComponent && line == "}":
			inComponent = false
		}
		if !inComponent {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "expression") {
			continue
		}
		quoted := trimmed[strings.Index(trimmed, "=")+1:]
		unquoted, err := strconv.Unquote(strings.TrimSpace(quoted))
		if err != nil {
			t.Fatalf("could not unquote the expression on %q: %v", trimmed, err)
		}
		expressions = append(expressions, unquoted)
	}
	return expressions
}

// alloyEmailExpression is the last expression in the component; the config
// documents the order (dsn, credentials, email) and the infra suite pins it.
func alloyEmailExpression(t *testing.T) string {
	t.Helper()

	expressions := alloyRedactionExpressions(t)
	if len(expressions) == 0 {
		t.Fatal("no redaction expressions found")
	}
	last := expressions[len(expressions)-1]
	if !strings.Contains(last, "@") {
		t.Fatalf("the last expression is not the email rule; the documented order changed: %s", last)
	}
	return last
}
