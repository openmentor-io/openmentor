package recaptcha

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/openmentor-io/openmentor/api/pkg/httpclient"
)

// Response represents the response from Google's reCAPTCHA verification API
type Response struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	ErrorCodes  []string `json:"error-codes"`
}

// Verifier handles reCAPTCHA verification
type Verifier struct {
	secretKey  string
	httpClient httpclient.Client
}

// NewVerifier creates a new reCAPTCHA verifier
func NewVerifier(secretKey string, httpClient httpclient.Client) *Verifier {
	return &Verifier{
		secretKey:  secretKey,
		httpClient: httpClient,
	}
}

// Verify verifies a reCAPTCHA token with Google's API
func (v *Verifier) Verify(token string) error {
	// Prepare form data
	data := url.Values{}
	data.Set("secret", v.secretKey)
	data.Set("response", token)

	// Send POST request to Google's verification endpoint
	resp, err := v.httpClient.Post(
		"https://www.google.com/recaptcha/api/siteverify",
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return fmt.Errorf("failed to verify recaptcha: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var result Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode recaptcha response: %w", err)
	}

	if !result.Success {
		return fmt.Errorf("recaptcha verification failed")
	}

	return nil
}
