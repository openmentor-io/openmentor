package services

// Instrumentation for refused usernames (D87). The rules themselves live in
// pkg/slug, which is a leaf package with no telemetry dependency — this is the
// service-layer half that records the decision.

import (
	"errors"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
)

// Surfaces a username can be refused on. A rejection costs the user very
// different things depending on which one it is: a lost signup, an abandoned
// rename, or a red hint they retype past.
const (
	usernameSurfaceRegistration = "registration"
	usernameSurfaceChange       = "change"
	usernameSurfaceAvailability = "availability"
)

// recordUsernameRejection counts and logs one refusal. The candidate itself is
// NOT logged: by this point it has passed the format rule, so it is a plain
// slug — which is usually the person's own name. The counter carries the shape
// of the problem and the log line places it in the request.
func recordUsernameRejection(surface string, err error) {
	reason := usernameRejectionReason(err)
	metrics.UsernameRejections.WithLabelValues(surface, reason).Inc()
	logger.Info("Username rejected",
		zap.String("surface", surface),
		zap.String("reason", reason),
	)
}

// usernameRejectionReason maps a pkg/slug error onto a bounded label value.
// ErrUsernameMentorDerivative WRAPS ErrUsernameReserved, so it has to be tested
// first — matched the other way round every derivative would count as a plain
// reserved word, and the rule the counter exists to measure would be invisible.
func usernameRejectionReason(err error) string {
	switch {
	case errors.Is(err, slug.ErrUsernameMentorDerivative):
		return "mentor_derivative"
	case errors.Is(err, slug.ErrUsernameReserved):
		return "reserved"
	case errors.Is(err, slug.ErrUsernameTooShort):
		return "too_short"
	case errors.Is(err, slug.ErrUsernameTooLong):
		return "too_long"
	case errors.Is(err, slug.ErrUsernameBadFormat):
		return "bad_format"
	default:
		return "other"
	}
}
