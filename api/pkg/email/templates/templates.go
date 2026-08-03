// Package templates holds the transactional email templates and renders
// them locally with Go's html/template and text/template.
//
// The templates are a 1:1 port of openmentor-func/lib/postbox/templates/*.ts
// (subject/html/text copied verbatim). They are embedded as JSON assets and
// exposed through a GetTemplate(name) registry that mirrors
// openmentor-func/lib/postbox/templates.ts.
//
// Rendering must not move back to SES: SES does not escape substituted values
// ("SES doesn't escape HTML content when rendering the HTML template for a
// message" —
// https://docs.aws.amazon.com/ses/latest/dg/send-personalized-email-advanced.html),
// so every user-controlled prop would be an HTML injection vector. Escaping
// the props at the boundary cannot replace this either, because one
// TemplateData blob feeds all three parts and the subject line and plaintext
// body would then carry literal entities. Hence Render(): html/template for
// the HTML part, text/template for the subject and text parts.
package templates

import (
	"embed"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	texttemplate "text/template"
)

//go:embed assets/*.json
var assetsFS embed.FS

// EmailTemplate is the raw template content: subject, HTML body and
// plain-text body, all containing {{placeholder}} markers.
type EmailTemplate struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// Rendered is a template with its placeholders substituted, ready to be sent
// as SES Simple content.
type Rendered struct {
	Subject string
	HTML    string
	Text    string
}

// compiled holds the three parsed parts of one template. Only the HTML part
// is an html/template: entity-escaping a subject header or a plaintext body
// would show the reader a literal "&amp;".
type compiled struct {
	subject *texttemplate.Template
	html    *htmltemplate.Template
	text    *texttemplate.Template
}

// placeholderRe matches the assets' {{name}} markers.
var placeholderRe = regexp.MustCompile(`\{\{([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// Placeholders returns the sorted, deduplicated prop names a template
// declares across its three parts. Exported for the tests in other packages
// that check a job supplies every prop its template needs: they must inventory
// the assets with the same pattern the loader rewrites, not a private copy of
// it that can drift narrower.
func Placeholders(tpl EmailTemplate) []string {
	seen := make(map[string]bool)
	names := make([]string, 0, 8)
	for _, section := range []string{tpl.Subject, tpl.HTML, tpl.Text} {
		for _, match := range placeholderRe.FindAllStringSubmatch(section, -1) {
			if !seen[match[1]] {
				seen[match[1]] = true
				names = append(names, match[1])
			}
		}
	}
	sort.Strings(names)
	return names
}

// registry maps template names to their content, mirroring EMAIL_TEMPLATES
// in openmentor-func/lib/postbox/templates.ts. compiledRegistry holds the
// same templates parsed; both are built eagerly so an asset that is missing,
// unparseable or has a syntax error fails the process at startup rather than
// the first send. Contextual escaping is *not* covered: html/template runs
// its escape analysis on first Execute, so an asset with, say, an unbalanced
// attribute quote parses clean here and fails when it is first rendered —
// which is what TestEveryTemplateRendersAgainstFixture exists to catch.
var registry, compiledRegistry = mustLoadTemplates()

func mustLoadTemplates() (map[string]EmailTemplate, map[string]compiled) {
	entries, err := fs.ReadDir(assetsFS, "assets")
	if err != nil {
		panic(fmt.Sprintf("email templates: failed to read embedded assets: %v", err))
	}

	loaded := make(map[string]EmailTemplate, len(entries))
	parsed := make(map[string]compiled, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".json")

		data, err := assetsFS.ReadFile("assets/" + entry.Name())
		if err != nil {
			panic(fmt.Sprintf("email templates: failed to read %s: %v", entry.Name(), err))
		}

		var tpl EmailTemplate
		if err := json.Unmarshal(data, &tpl); err != nil {
			panic(fmt.Sprintf("email templates: failed to parse %s: %v", entry.Name(), err))
		}
		if tpl.Subject == "" || tpl.HTML == "" || tpl.Text == "" {
			panic(fmt.Sprintf("email templates: %s is missing subject, html or text", entry.Name()))
		}

		c, err := compile(name, tpl)
		if err != nil {
			panic(fmt.Sprintf("email templates: failed to compile %s: %v", entry.Name(), err))
		}

		loaded[name] = tpl
		parsed[name] = c
	}

	return loaded, parsed
}

// toGoTemplate rewrites the assets' {{name}} markers into Go's {{.name}} map
// lookup so the JSON assets stay byte-identical to the func app's originals.
// Indexing with {{.name}} rather than {{index . "name"}} is deliberate: only
// the field form honors missingkey=error.
func toGoTemplate(s string) string {
	return placeholderRe.ReplaceAllString(s, "{{.${1}}}")
}

// compile parses the three parts of a template. missingkey=error makes a
// prop the template needs but the caller did not supply a send-time error
// instead of the empty string SES would have silently substituted.
func compile(name string, tpl EmailTemplate) (compiled, error) {
	subject, err := texttemplate.New(name + ":subject").Option("missingkey=error").Parse(toGoTemplate(tpl.Subject))
	if err != nil {
		return compiled{}, fmt.Errorf("subject: %w", err)
	}
	html, err := htmltemplate.New(name + ":html").Option("missingkey=error").Parse(toGoTemplate(tpl.HTML))
	if err != nil {
		return compiled{}, fmt.Errorf("html: %w", err)
	}
	text, err := texttemplate.New(name + ":text").Option("missingkey=error").Parse(toGoTemplate(tpl.Text))
	if err != nil {
		return compiled{}, fmt.Errorf("text: %w", err)
	}
	return compiled{subject: subject, html: html, text: text}, nil
}

// executor is the shared Execute method of html/template and text/template.
type executor interface {
	Execute(w io.Writer, data any) error
}

func execute(t executor, part string, props map[string]interface{}) (string, error) {
	var buf strings.Builder
	if err := t.Execute(&buf, props); err != nil {
		return "", fmt.Errorf("%s: %w", part, err)
	}
	return buf.String(), nil
}

// Render substitutes props into a template's three parts. Values landing in
// the HTML part are escaped for the context they land in — an href gets URL
// filtering, so a javascript: or data: URL becomes "#ZgotmplZ" — while the
// subject and text parts are substituted verbatim. Pass template.HTML for
// the server-built markup fragments (decline_info, requests_list) that must
// not be escaped.
func Render(name string, props map[string]interface{}) (Rendered, error) {
	c, ok := compiledRegistry[name]
	if !ok {
		return Rendered{}, fmt.Errorf("template not found: %s", name)
	}
	if props == nil {
		props = map[string]interface{}{}
	}

	subject, err := execute(c.subject, "subject", props)
	if err != nil {
		return Rendered{}, fmt.Errorf("failed to render %s: %w", name, err)
	}
	html, err := execute(c.html, "html", props)
	if err != nil {
		return Rendered{}, fmt.Errorf("failed to render %s: %w", name, err)
	}
	text, err := execute(c.text, "text", props)
	if err != nil {
		return Rendered{}, fmt.Errorf("failed to render %s: %w", name, err)
	}

	return Rendered{Subject: subject, HTML: html, Text: text}, nil
}

// GetTemplate returns a template by name. It mirrors getTemplate() in
// openmentor-func/lib/postbox/templates.ts and returns an error when the
// template does not exist.
func GetTemplate(name string) (EmailTemplate, error) {
	tpl, ok := registry[name]
	if !ok {
		return EmailTemplate{}, fmt.Errorf("template not found: %s", name)
	}
	return tpl, nil
}

// Names returns the sorted list of registered template names.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
