package templates

import (
	"strings"
	"testing"
)

// expectedTemplates mirrors EMAIL_TEMPLATES in
// openmentor-func/lib/postbox/templates.ts: every template name that the
// func app registers must resolve here, with the same placeholders.
var expectedTemplates = map[string][]string{
	"mentor-confirm-email": {"first_name", "confirm_url"},
	"new-mentor-approved":  {"first_name", "mentor_profile_url", "mentor_profile_share_url", "discord_invite_link"},
	"new-mentor-declined":  {"first_name"},
	"new-mentor-returned":  {"first_name", "reviewer_note", "edit_url"},
	"new-mentor":           {"first_name"},
	"new-mentor-duplicate": {"first_name"},
	"new-request-calendly": {"calendly_url", "first_name", "mentor_name", "request_details", "request_price"},
	"new-request-mentor":   {"mentee_contact", "mentee_email", "mentee_name", "mentee_request", "mentor_name", "request_url"},
	"new-request":          {"first_name", "mentor_name", "request_details", "request_price"},
	"mentor-login":         {"login_url", "mentor_name"},
	// H4: the review link is now a whole URL carrying a single-use capability in
	// its FRAGMENT, built by worker.issueReviewInvitation. It replaced
	// `request_id`, which this template used to interpolate into a query string —
	// which is what made the primary key itself the review capability.
	"session-complete":          {"first_name", "mentor_name", "review_url"},
	"session-declined":          {"decline_info", "decline_info_text", "first_name", "mentor_name"},
	"new-review":                {"first_name", "mentee_name", "review_text"},
	"new-mentor-moderator":      {"mentor_email", "mentor_job", "mentor_name"},
	"new-request-moderator":     {"admin_url", "mentee_level", "mentee_name", "mentor_name", "request_url"},
	"pending-requests-reminder": {"mentor_name", "pending_count", "requests_list", "requests_list_text"},
	"status-update-reminder":    {"mentor_name", "requests_list", "requests_list_text"},
	"profile-deactivated":       {"mentor_name"},

	// Post-func-app additions (no legacy counterpart): profile-migrated is
	// sent by the getmentor->openmentor migration tooling through the
	// worker's /jobs/profile-migrated endpoint.
	"profile-migrated": {"first_name", "mentor_profile_url"},

	// Profile deletion (D70). Both are sent for a mentor-initiated AND an
	// admin-initiated action. profile-deleted takes retention_days from
	// WORKER_PROFILE_PURGE_RETENTION_DAYS rather than hardcoding "30 days", so
	// changing the window cannot leave the email telling people the wrong
	// deadline for asking us to undo it.
	"profile-deleted":  {"first_name", "retention_days"},
	"profile-restored": {"first_name", "login_url"},
}

func TestRegistryCompleteness(t *testing.T) {
	for name := range expectedTemplates {
		tpl, err := GetTemplate(name)
		if err != nil {
			t.Errorf("GetTemplate(%q) failed: %v", name, err)
			continue
		}
		if tpl.Subject == "" {
			t.Errorf("template %q has an empty subject", name)
		}
		if tpl.HTML == "" {
			t.Errorf("template %q has an empty html body", name)
		}
		if tpl.Text == "" {
			t.Errorf("template %q has an empty text body", name)
		}
	}
}

func TestNoUnexpectedTemplates(t *testing.T) {
	for _, name := range Names() {
		if _, ok := expectedTemplates[name]; !ok {
			t.Errorf("unexpected template %q registered (not in the func app's registry)", name)
		}
	}
	if got, want := len(Names()), len(expectedTemplates); got != want {
		t.Errorf("registered %d templates, want %d", got, want)
	}
}

func TestTemplatePlaceholders(t *testing.T) {
	for name, wantPlaceholders := range expectedTemplates {
		tpl, err := GetTemplate(name)
		if err != nil {
			t.Errorf("GetTemplate(%q) failed: %v", name, err)
			continue
		}

		found := Placeholders(tpl)
		for _, placeholder := range wantPlaceholders {
			if !contains(found, placeholder) {
				t.Errorf("template %q is missing placeholder {{%s}}", name, placeholder)
			}
		}
		for _, placeholder := range found {
			if !contains(wantPlaceholders, placeholder) {
				t.Errorf("template %q has unexpected placeholder {{%s}}", name, placeholder)
			}
		}
	}
}

func TestGetTemplateUnknownName(t *testing.T) {
	_, err := GetTemplate("does-not-exist")
	if err == nil {
		t.Fatal("GetTemplate with an unknown name should return an error")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should mention the template name, got: %v", err)
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
