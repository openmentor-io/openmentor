package email

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// sentinelRecipient is the address C11 is about. RFC 2606 example.com, so this
// file, its failure output and any CI log that quotes it carry no real address.
const sentinelRecipient = "mentee.person@example.com"

// reviewProps are the props the new-review template needs — the template from
// the production line the audit found. Every value is obviously fake.
var reviewProps = map[string]interface{}{
	"first_name":  "Alice",
	"mentee_name": "Sam Example",
	"review_text": "helpful session",
}

// TestSendNeverLogsTheRecipient is the C11 regression test. The audit found this
// exact line in production Loki:
//
//	{"level":"info","caller":"email/sender.go:160","msg":"SES email sent",
//	 "template":"new-review","recipient":"<a real user's address>", …}
//
// so the test drives the real Send through the real logger pipeline — including
// the redacting core Initialize installs — and asserts the address is in no line
// on either the success or the failure path.
func TestSendNeverLogsTheRecipient(t *testing.T) {
	paths := []struct {
		name    string
		sesErr  error
		wantErr bool
	}{
		{"success", nil, false},
		{
			// SES quotes the destination back in its own rejection, which is why
			// masking the field alone was never enough.
			name:    "ses rejects the destination",
			sesErr:  fmt.Errorf("MessageRejected: the following identities failed the check in region eu-central-1: %s", sentinelRecipient),
			wantErr: true,
		},
	}

	for _, path := range paths {
		t.Run(path.name, func(t *testing.T) {
			logs := observeRedactedLogs(t)

			sender := NewSenderWithClient(&mockSESClient{err: path.sesErr}, "production", "")
			_, err := sender.Send(context.Background(), Message{
				TemplateName: "new-review",
				Recipient:    sentinelRecipient,
				Props:        reviewProps,
			})
			if (err != nil) != path.wantErr {
				t.Fatalf("Send error = %v, wantErr = %v", err, path.wantErr)
			}

			if len(logs.All()) == 0 {
				t.Fatal("nothing was logged: the test drove nothing and proves nothing")
			}
			assertNoSentinel(t, logs)
		})
	}
}

// TestSendStillIdentifiesTheMessage is the other half of C11's requirement: the
// point was to stop logging addresses, not to make a failed send unattributable.
func TestSendStillIdentifiesTheMessage(t *testing.T) {
	logs := observeRedactedLogs(t)

	sender := NewSenderWithClient(&mockSESClient{}, "production", "")
	if _, err := sender.Send(context.Background(), Message{
		TemplateName: "new-review",
		Recipient:    sentinelRecipient,
		Props:        reviewProps,
	}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	fields := logs.All()[0].ContextMap()
	// message_id is SES's own handle for this send — what an operator quotes at
	// support — and template says which email it was.
	if fields["message_id"] != "test-message-id" {
		t.Errorf("message_id = %v, want the SES id: a failed send has to stay identifiable", fields["message_id"])
	}
	if fields["template"] != "new-review" {
		t.Errorf("template = %v, want new-review", fields["template"])
	}
	// The masked recipient keeps the domain, so a domain-wide deliverability
	// problem is still visible, and stays stable per address so two lines about
	// the same person can still be tied together.
	if fields["recipient"] != "m***@example.com" {
		t.Errorf("recipient = %v, want the masked form m***@example.com", fields["recipient"])
	}
}

// TestSendMasksWithoutTheRedactingCore pins the CALL SITE on its own. The two
// tests above pass with either layer in place — which is what defense in depth
// means, but it also means neither would notice if the explicit masking in Send
// were reverted. This one uses a bare observer, so it fails if the field stops
// being masked at the source.
func TestSendMasksWithoutTheRedactingCore(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Log
	logger.Log = zap.New(core)
	t.Cleanup(func() { logger.Log = previous })

	sender := NewSenderWithClient(&mockSESClient{}, "production", "")
	if _, err := sender.Send(context.Background(), Message{
		TemplateName: "new-review",
		Recipient:    sentinelRecipient,
		Props:        reviewProps,
	}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if got := logs.All()[0].ContextMap()["recipient"]; got != "m***@example.com" {
		t.Errorf("recipient = %v, want it masked by Send itself and not only by the logger", got)
	}
}

// TestResolveRecipientNeverLogsEitherAddress covers the DEV_EMAIL_OVERRIDE line.
// It only fires outside production, but staging ships to the same Grafana tenant,
// and the "original" address there is a real user's.
func TestResolveRecipientNeverLogsEitherAddress(t *testing.T) {
	logs := observeRedactedLogs(t)

	const override = "dev.inbox@example.org"
	if got := ResolveRecipient(sentinelRecipient, override, "staging"); got != override {
		t.Fatalf("ResolveRecipient = %q, want the override %q", got, override)
	}

	if len(logs.All()) == 0 {
		t.Fatal("nothing was logged: the override branch did not run")
	}
	assertNoSentinel(t, logs)
	if strings.Contains(renderLogs(t, logs), override) {
		t.Error("the override address was logged in the clear")
	}
}

// observeRedactedLogs swaps the global logger for a recording one wrapped in the
// SAME redacting core Initialize installs, so the test exercises the production
// pipeline rather than a bare observer that would hide the core's contribution.
func observeRedactedLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()

	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Log
	logger.Log = zap.New(logger.NewRedactingCore(core))
	t.Cleanup(func() { logger.Log = previous })

	return logs
}

func assertNoSentinel(t *testing.T, logs *observer.ObservedLogs) {
	t.Helper()

	rendered := renderLogs(t, logs)
	if strings.Contains(rendered, sentinelRecipient) {
		t.Errorf("a log line carries the recipient: %s", rendered)
	}
	// The local part on its own is enough to identify someone, so check it
	// separately from the whole address.
	if strings.Contains(rendered, "mentee.person") {
		t.Errorf("a log line carries the recipient's local part: %s", rendered)
	}
}

func renderLogs(t *testing.T, logs *observer.ObservedLogs) string {
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
