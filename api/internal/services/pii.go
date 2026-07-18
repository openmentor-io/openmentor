package services

import "strings"

// maskEmail redacts an email for logging: keeps the first local-part character
// and the domain, masking the rest (e.g. "john@example.com" -> "j***@example.com").
// SECURITY/GDPR: avoids writing raw addresses to logs, which are shipped to
// Grafana and would otherwise be an enumeration oracle for anyone with log
// access (L3). Non-address input is fully masked.
func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	local, domain := email[:at], email[at:]
	if len(local) == 1 {
		return "*" + domain
	}
	return local[:1] + "***" + domain
}
