// Package templates holds the transactional email templates rendered
// server-side by the AWS SESv2 API using {{placeholder}} syntax.
//
// The templates are a 1:1 port of openmentor-func/lib/postbox/templates/*.ts
// (subject/html/text copied verbatim). They are embedded as JSON assets and
// exposed through a GetTemplate(name) registry that mirrors
// openmentor-func/lib/postbox/templates.ts.
package templates

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed assets/*.json
var assetsFS embed.FS

// EmailTemplate is the SES inline template content: subject, HTML body and
// plain-text body, all containing {{placeholder}} markers substituted
// server-side by SES from the message's TemplateData.
type EmailTemplate struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
}

// registry maps template names to their content, mirroring EMAIL_TEMPLATES
// in openmentor-func/lib/postbox/templates.ts.
var registry = mustLoadTemplates()

func mustLoadTemplates() map[string]EmailTemplate {
	entries, err := fs.ReadDir(assetsFS, "assets")
	if err != nil {
		panic(fmt.Sprintf("email templates: failed to read embedded assets: %v", err))
	}

	loaded := make(map[string]EmailTemplate, len(entries))
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

		loaded[name] = tpl
	}

	return loaded
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
