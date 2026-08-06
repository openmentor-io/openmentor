package services

import (
	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
)

// newCaptchaVerifier builds the Turnstile verifier for the four services that
// gate a public write with a captcha. One constructor because the settings must
// be identical across all four: wiring the hostname/action pinning into three of
// them would leave the fourth form as the one an attacker farms tokens against.
func newCaptchaVerifier(cfg *config.Config, httpClient httpclient.Client) *turnstile.Verifier {
	return turnstile.NewVerifier(turnstile.Options{
		SecretKey:        cfg.Turnstile.SecretKey,
		ExpectedHostname: cfg.Turnstile.ExpectedHostname,
		ExpectedAction:   cfg.Turnstile.ExpectedAction,
	}, httpClient)
}
