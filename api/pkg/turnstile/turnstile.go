package turnstile

import (
	"encoding/json"
	"fmt"
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
	ErrorCodes  []string `json:"error-codes"`
}

// Verifier handles Turnstile verification
type Verifier struct {
	secretKey  string
	httpClient httpclient.Client
}

// NewVerifier creates a new Turnstile verifier
func NewVerifier(secretKey string, httpClient httpclient.Client) *Verifier {
	return &Verifier{
		secretKey:  secretKey,
		httpClient: httpClient,
	}
}

// Verify verifies a Turnstile token with Cloudflare's siteverify API
func (v *Verifier) Verify(token string) error {
	// Prepare form data
	data := url.Values{}
	data.Set("secret", v.secretKey)
	data.Set("response", token)

	// Send POST request to Cloudflare's verification endpoint
	resp, err := v.httpClient.Post(
		VerifyURL,
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to verify turnstile token: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode turnstile response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("turnstile verification failed")
	}

	return nil
}
