package email

// Message describes a single transactional email to send.
//
// It mirrors the EmailMessage abstract class from
// openmentor-func/lib/sendgrid/messages/EmailMessage.ts: a template
// identifier, a recipient and the template properties (TemplateData)
// substituted into {{placeholder}} markers server-side by SES.
//
// Stage 2 will add typed constructors for each business message
// (new-mentor, new-request, ...) on top of this struct.
type Message struct {
	// TemplateName is the registry key of the template to render,
	// e.g. "mentor-login" (see pkg/email/templates).
	TemplateName string

	// Recipient is the destination email address. It may be rerouted by
	// DEV_EMAIL_OVERRIDE in non-production environments.
	Recipient string

	// Props holds the template placeholder values, e.g.
	// {"mentor_name": "...", "login_url": "..."}.
	Props map[string]interface{}
}
