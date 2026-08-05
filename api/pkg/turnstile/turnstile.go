package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
)

// VerifyURL is Cloudflare's Turnstile siteverify endpoint.
const VerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Response represents the response from Cloudflare's Turnstile siteverify API
type Response struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	Action      string   `json:"action"`
	ErrorCodes  []string `json:"error-codes"`
}

// Options configures a Verifier. ExpectedHostname and ExpectedAction are
// optional; when set they are compared against what siteverify echoes back, so a
// token solved on someone else's page or harvested from another form cannot be
// replayed here. Empty means "accept whatever Cloudflare reports".
type Options struct {
	SecretKey        string
	ExpectedHostname string
	ExpectedAction   string
}

// Verifier handles Turnstile verification
type Verifier struct {
	opts       Options
	httpClient httpclient.Client
}

// NewVerifier creates a new Turnstile verifier
func NewVerifier(opts Options, httpClient httpclient.Client) *Verifier {
	return &Verifier{
		opts:       opts,
		httpClient: httpClient,
	}
}

// Verify verifies a Turnstile token with Cloudflare's siteverify API.
//
// ctx is not decoration: this runs inline on every public write (contact,
// registration, review, migration opt-in), so without it a slow or hung
// Cloudflare holds the request goroutine — and with it the caller's admission
// slot and DB connection — for the HTTP client's full 30s timeout, long after
// the browser gave up.
func (v *Verifier) Verify(ctx context.Context, token string) error {
	data := url.Values{}
	data.Set("secret", v.opts.SecretKey)
	data.Set("response", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, VerifyURL,
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to build turnstile request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to verify turnstile token: %w", err)
	}
	defer resp.Body.Close()

	// A non-2xx is an outage or a rejected request, not a verdict. Its body is
	// usually an HTML error page, which decodes into the zero Response — i.e.
	// Success:false — so an outage used to be reported to the user as "your
	// captcha is invalid", sending them round the form again with no way to win.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("turnstile siteverify returned HTTP %d", resp.StatusCode)
	}

	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode turnstile response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("turnstile verification failed")
	}

	// Cloudflare attests that the token is genuine and unused; only WE know which
	// site and which form it was supposed to come from.
	if want := v.opts.ExpectedHostname; want != "" && !strings.EqualFold(want, result.Hostname) {
		return fmt.Errorf("turnstile hostname mismatch: token was solved on a different site")
	}
	if want := v.opts.ExpectedAction; want != "" && want != result.Action {
		return fmt.Errorf("turnstile action mismatch: token was issued for a different form")
	}

	return nil
}
