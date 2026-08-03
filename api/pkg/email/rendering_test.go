package email

import (
	"html/template"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// evilPayload closes the element it lands in and opens a phishing link. It is
// the payload the audit used to demonstrate that SES-side rendering delivered
// it as live markup ("SES doesn't escape HTML content when rendering the HTML
// template for a message").
const evilPayload = `x</div><a href="https://evil.example/pay">Pay now</a><div>`

// renderParts renders a template through the real sender and returns the
// three parts SES would receive.
func renderParts(t *testing.T, name string, props map[string]interface{}) (subject, htmlBody, textBody string) {
	t.Helper()
	s := NewSenderWithClient(nil, "production", "")
	input, err := s.BuildSendEmailInput(Message{
		TemplateName: name,
		Recipient:    "victim@example.com",
		Props:        props,
	})
	if err != nil {
		t.Fatalf("BuildSendEmailInput(%s): %v", name, err)
	}
	simple := input.Content.Simple
	if simple == nil {
		t.Fatalf("BuildSendEmailInput(%s) did not produce Simple content", name)
	}
	return aws.ToString(simple.Subject.Data),
		aws.ToString(simple.Body.Html.Data),
		aws.ToString(simple.Body.Text.Data)
}

// TestUserPropsCannotEmitMarkup covers the three templates that interpolate
// free text an unauthenticated visitor controls.
func TestUserPropsCannotEmitMarkup(t *testing.T) {
	cases := []struct {
		template string
		prop     string
		props    map[string]interface{}
	}{
		{"new-request-mentor", "mentee_request", map[string]interface{}{
			"mentee_contact": "tg: @evil",
			"mentee_email":   "evil@example.com",
			"mentee_name":    "Evil Mentee",
			"mentee_request": evilPayload,
			"mentor_name":    "Alice",
			"request_url":    "https://openmentor.io/mentor",
		}},
		{"new-request", "request_details", map[string]interface{}{
			"first_name":      "Alice",
			"mentor_name":     "Bob",
			"request_details": evilPayload,
			"request_price":   "free",
		}},
		{"new-review", "review_text", map[string]interface{}{
			"first_name":  "Alice",
			"mentee_name": "Evil Mentee",
			"review_text": evilPayload,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			_, htmlBody, _ := renderParts(t, tc.template, tc.props)

			if strings.Contains(htmlBody, `<a href="https://evil.example/pay">`) {
				t.Error("the injected anchor is live markup in the HTML body")
			}
			// Nothing the payload contributed may survive as a tag: every
			// angle bracket it carried has to come back as an entity.
			for _, fragment := range []string{"</div>", "<a ", "<div>"} {
				if strings.Contains(htmlBody, tc.prop+fragment) {
					t.Errorf("HTML body contains raw %q from the payload", fragment)
				}
			}
			if !strings.Contains(htmlBody, "&lt;/div&gt;&lt;a href=") {
				t.Errorf("payload was not entity-escaped in the HTML body; got:\n%s", window(htmlBody, "evil.example"))
			}
			if strings.Contains(htmlBody, "{{") {
				t.Error("HTML body still contains unsubstituted markers")
			}
		})
	}
}

// TestSubjectAndTextCarryNoEntities is the reason rendering happens locally
// rather than by escaping props at the boundary: SES applies one TemplateData
// blob to all three parts, so escaping for the HTML part would put literal
// "&amp;" in the inbox subject line and in every plaintext body.
func TestSubjectAndTextCarryNoEntities(t *testing.T) {
	names := []string{"Tom & Jerry", "O'Brien", `Ann "Annie" Lee`, "Müller & Söhne"}
	entities := []string{"&amp;", "&#39;", "&lt;", "&gt;", "&#34;", "&quot;", "&#43;"}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			subject, _, textBody := renderParts(t, "new-request", map[string]interface{}{
				"first_name":      name,
				"mentor_name":     name,
				"request_details": "I want to learn C++ & Go (rates < $50)",
				"request_price":   "5 < 10",
			})

			for part, got := range map[string]string{"subject": subject, "text": textBody} {
				if !strings.Contains(got, name) {
					t.Errorf("%s does not contain %q verbatim: %q", part, name, got)
				}
				for _, entity := range entities {
					if strings.Contains(got, entity) {
						t.Errorf("%s contains the HTML entity %q: %q", part, entity, got)
					}
				}
			}
			if !strings.Contains(textBody, "C++ & Go (rates < $50)") {
				t.Errorf("text body mangled the request details: %q", textBody)
			}
		})
	}
}

// TestCalendarURLSchemeIsNeutralized covers what per-prop escaping cannot do:
// escaping the characters of a scheme does not stop the browser from
// following it. html/template filters the whole URL by scheme instead,
// case-insensitively.
func TestCalendarURLSchemeIsNeutralized(t *testing.T) {
	render := func(t *testing.T, calendarURL string) string {
		t.Helper()
		_, htmlBody, _ := renderParts(t, "new-request-calendly", map[string]interface{}{
			"calendly_url":    calendarURL,
			"first_name":      "Alice",
			"mentor_name":     "Bob",
			"request_details": "hello",
			"request_price":   "free",
		})
		return htmlBody
	}

	for _, dangerous := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"data:text/html,<script>alert(1)</script>",
	} {
		t.Run(dangerous, func(t *testing.T) {
			htmlBody := render(t, dangerous)
			if !strings.Contains(htmlBody, `href="#ZgotmplZ"`) {
				t.Errorf("dangerous URL was not filtered: %s", window(htmlBody, "href="))
			}
			if strings.Contains(htmlBody, "alert(1)") {
				t.Error("the dangerous URL still reaches the rendered body")
			}
		})
	}

	t.Run("valid https URL survives", func(t *testing.T) {
		htmlBody := render(t, "https://ok.example/x?a=1&b=2")
		// The & is entity-encoded, which is how it must be written inside an
		// HTML attribute; email clients resolve it back to "&".
		if !strings.Contains(htmlBody, `href="https://ok.example/x?a=1&amp;b=2"`) {
			t.Errorf("valid URL did not survive: %s", window(htmlBody, "ok.example"))
		}
	})
}

// TestServerBuiltFragmentsStayMarkup covers the two props that intentionally
// carry HTML. They are template.HTML at their producing sites, so the
// template must emit them as markup rather than escaping them into visible
// tags — while the user text the producer interpolated stays escaped.
func TestServerBuiltFragmentsStayMarkup(t *testing.T) {
	t.Run("decline_info", func(t *testing.T) {
		_, htmlBody, _ := renderParts(t, "session-declined", map[string]interface{}{
			"first_name":  "Alice",
			"mentor_name": "Bob",
			"decline_info": template.HTML(`<br><br><div style="font-family: inherit; text-align: inherit">` +
				`<strong>Comment:</strong> Try again &lt;soon&gt;</div>`),
			"decline_info_text": "Comment: Try again <soon>",
		})

		if !strings.Contains(htmlBody, `<div style="font-family: inherit; text-align: inherit"><strong>Comment:</strong>`) {
			t.Errorf("decline_info was escaped instead of emitted as markup: %s", window(htmlBody, "Comment:"))
		}
		if strings.Contains(htmlBody, "&lt;div style=") {
			t.Error("decline_info markup was double-escaped")
		}
		// The mentor's comment inside the fragment stays escaped.
		if !strings.Contains(htmlBody, "Try again &lt;soon&gt;") {
			t.Error("the escaped comment inside the fragment was altered")
		}
	})

	t.Run("requests_list", func(t *testing.T) {
		_, htmlBody, _ := renderParts(t, "status-update-reminder", map[string]interface{}{
			"mentor_name":        "Bob",
			"requests_list":      template.HTML(`<ul style="padding-left: 20px; margin: 0;"><li>Mentee &amp; Co</li></ul>`),
			"requests_list_text": "- Mentee & Co",
		})

		if !strings.Contains(htmlBody, `<ul style="padding-left: 20px; margin: 0;"><li>Mentee &amp; Co</li></ul>`) {
			t.Errorf("requests_list was escaped instead of emitted as markup: %s", window(htmlBody, "Mentee"))
		}
	})

	// A plain string in the same prop is escaped: the type is what grants the
	// exemption, so a future caller cannot inject markup by forgetting it.
	t.Run("plain string in an HTML prop is escaped", func(t *testing.T) {
		_, htmlBody, _ := renderParts(t, "status-update-reminder", map[string]interface{}{
			"mentor_name":        "Bob",
			"requests_list":      `<ul><li>` + evilPayload + `</li></ul>`,
			"requests_list_text": "- x",
		})
		if strings.Contains(htmlBody, "<ul><li>") {
			t.Error("an untyped string prop was emitted as markup")
		}
		if !strings.Contains(htmlBody, "&lt;ul&gt;&lt;li&gt;") {
			t.Errorf("an untyped string prop was not escaped: %s", window(htmlBody, "evil.example"))
		}
	})
}

// TestBuildSendEmailInputNeverSendsTemplateData is the structural guard for
// the fix: if anyone reintroduces SES-side rendering, the escaping above is
// silently bypassed.
func TestBuildSendEmailInputNeverSendsTemplateData(t *testing.T) {
	s := NewSenderWithClient(nil, "production", "")
	input, err := s.BuildSendEmailInput(Message{
		TemplateName: "profile-deactivated",
		Recipient:    "mentor@example.com",
		Props:        map[string]interface{}{"mentor_name": "Jane"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.Content.Template != nil {
		t.Error("Content.Template must stay nil; SES must not render templates")
	}
	if input.Content.Raw != nil {
		t.Error("Content.Raw must stay nil")
	}
}

// window returns the text around needle, to keep failure output readable.
func window(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return s
	}
	lo, hi := i-200, i+200
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return "..." + s[lo:hi] + "..."
}
