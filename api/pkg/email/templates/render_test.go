package templates

import (
	"strings"
	"testing"
)

// TestEveryTemplateCompiles proves the eager load did its job: every embedded
// asset is parsed, so a stray or malformed action fails the process at
// startup instead of the first send.
func TestEveryTemplateCompiles(t *testing.T) {
	if got, want := len(compiledRegistry), len(registry); got != want {
		t.Fatalf("compiled %d templates, loaded %d", got, want)
	}
	for _, name := range Names() {
		c, ok := compiledRegistry[name]
		if !ok {
			t.Errorf("template %q was loaded but not compiled", name)
			continue
		}
		if c.subject == nil || c.html == nil || c.text == nil {
			t.Errorf("template %q has an unparsed part", name)
		}
	}
}

// TestEveryTemplateRendersAgainstFixture renders all three parts of every
// asset with a value for each placeholder the asset declares. Under
// missingkey=error this fails for any placeholder the contract in
// expectedTemplates does not list.
func TestEveryTemplateRendersAgainstFixture(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			props := make(map[string]interface{}, len(expectedTemplates[name]))
			for _, placeholder := range expectedTemplates[name] {
				props[placeholder] = "fixture-" + placeholder
			}

			rendered, err := Render(name, props)
			if err != nil {
				t.Fatalf("Render(%s): %v", name, err)
			}
			for part, got := range map[string]string{
				"subject": rendered.Subject, "html": rendered.HTML, "text": rendered.Text,
			} {
				if got == "" {
					t.Errorf("%s part rendered empty", part)
				}
				if strings.Contains(got, "{{") {
					t.Errorf("%s part still contains an unsubstituted marker: %q", part, got)
				}
			}
		})
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	_, err := Render("does-not-exist", nil)
	if err == nil {
		t.Fatal("Render with an unknown name should return an error")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should mention the template name, got: %v", err)
	}
}

func TestRenderRejectsMissingPlaceholder(t *testing.T) {
	// mentor-login needs mentor_name and login_url.
	_, err := Render("mentor-login", map[string]interface{}{"mentor_name": "Jane"})
	if err == nil {
		t.Fatal("Render should fail when a placeholder has no prop")
	}
	if !strings.Contains(err.Error(), "login_url") {
		t.Errorf("error should name the missing placeholder, got: %v", err)
	}
}

func TestToGoTemplateRewritesPlaceholders(t *testing.T) {
	cases := map[string]string{
		"Hi {{first_name}} — {{mentor_name}}": "Hi {{.first_name}} — {{.mentor_name}}",
		`<a href="{{login_url}}">go</a>`:      `<a href="{{.login_url}}">go</a>`,
		"a CSS rule { padding: 0 } stays":     "a CSS rule { padding: 0 } stays",
		"{{ spaced }} is not a placeholder":   "{{ spaced }} is not a placeholder",
		"{{Mixed_Case1}} rewrites":            "{{.Mixed_Case1}} rewrites",
	}
	for in, want := range cases {
		if got := toGoTemplate(in); got != want {
			t.Errorf("toGoTemplate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompileRejectsABrokenTemplate(t *testing.T) {
	// A stray action is what a hand-edited asset would most likely introduce;
	// it must be a compile error, which mustLoadTemplates turns into a
	// startup panic.
	broken := []struct {
		name string
		tpl  EmailTemplate
	}{
		{"bad subject", EmailTemplate{Subject: "{{ if }}", HTML: "ok", Text: "ok"}},
		{"bad html", EmailTemplate{Subject: "ok", HTML: "{{ range }}", Text: "ok"}},
		{"bad text", EmailTemplate{Subject: "ok", HTML: "ok", Text: "{{ end }}"}},
	}
	for _, tc := range broken {
		if _, err := compile("test", tc.tpl); err == nil {
			t.Errorf("compile accepted a %s", tc.name)
		}
	}
}
