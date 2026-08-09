package email

import (
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// ResolveRecipient applies the central DEV_EMAIL_OVERRIDE handling.
//
// In non-production environments (appEnv != "production"), a non-empty
// override reroutes ALL outgoing emails to that address instead of the real
// recipients. This makes email flows verifiable against a dev inbox without
// spamming real users. The override is intentionally ignored in production.
//
// This mirrors openmentor-func/lib/email/recipientOverride.ts.
func ResolveRecipient(recipient, override, appEnv string) string {
	if override != "" && appEnv != "production" {
		// Masked even though this branch is non-production only: staging logs land
		// in the same Grafana tenant as production ones, and the real recipient
		// here is a real user (C11).
		logger.Info("DEV_EMAIL_OVERRIDE active: rerouting email",
			zap.String("original_recipient", redact.Email(recipient)),
			zap.String("override_recipient", redact.Email(override)),
		)
		return override
	}

	return recipient
}
