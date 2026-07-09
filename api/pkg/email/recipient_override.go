package email

import (
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
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
		logger.Info("DEV_EMAIL_OVERRIDE active: rerouting email",
			zap.String("original_recipient", recipient),
			zap.String("override_recipient", override),
		)
		return override
	}

	return recipient
}
