package turnstile_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/turnstile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSiteverify stands in for Cloudflare at the httpclient.Client boundary — the
// same seam production uses — and records the request so the tests can assert
// what was actually sent. It deliberately does NOT re-implement siteverify's
// logic: each case supplies the exact response it wants to model.
type fakeSiteverify struct {
	status int
	body   string
	err    error

	gotRequest *http.Request
	gotBody    string
	calls      int
}

func (f *fakeSiteverify) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	f.gotRequest = req
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		f.gotBody = string(raw)
	}
	// Honor the request context the way a real transport does: a canceled
	// context must fail before any response is produced.
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
	}, nil
}

func (f *fakeSiteverify) Post(_, _ string, _ io.Reader) (*http.Response, error) {
	panic("turnstile must use Do: Post cannot carry a context")
}

func (f *fakeSiteverify) Get(string) (*http.Response, error) {
	panic("unexpected GET to siteverify")
}

const successBody = `{"success": true, "challenge_ts": "2026-08-04T00:00:00Z", ` +
	`"hostname": "openmentor.io", "action": "contact-mentor"}`

func TestVerify_Success(t *testing.T) {
	fake := &fakeSiteverify{body: successBody}
	verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "test-secret-key"}, fake)

	require.NoError(t, verifier.Verify(context.Background(), "valid-token"))

	require.NotNil(t, fake.gotRequest)
	assert.Equal(t, http.MethodPost, fake.gotRequest.Method)
	assert.Equal(t, turnstile.VerifyURL, fake.gotRequest.URL.String())
	assert.Equal(t, "application/x-www-form-urlencoded",
		fake.gotRequest.Header.Get("Content-Type"))
	assert.Contains(t, fake.gotBody, "response=valid-token")
}

// TestVerify_ContextIsPropagated is the point of taking a ctx: this runs inline
// on every public write, so a client that has hung up (or a request past its
// deadline) must not keep the goroutine, admission slot and DB connection alive
// for the HTTP client's full 30s timeout.
func TestVerify_ContextIsPropagated(t *testing.T) {
	fake := &fakeSiteverify{body: successBody}
	verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "k"}, fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := verifier.Verify(ctx, "token")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestVerify_NonSuccessStatusIsNotAVerdict: siteverify's 5xx body is an HTML
// error page, which decodes into the zero Response — i.e. Success:false. Reading
// that as "the captcha is invalid" told the user to solve it again during a
// Cloudflare outage, which they could never do successfully.
func TestVerify_NonSuccessStatusIsNotAVerdict(t *testing.T) {
	statuses := []int{http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusFound}
	for _, status := range statuses {
		fake := &fakeSiteverify{status: status, body: "<html>Cloudflare</html>"}
		verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "k"}, fake)

		err := verifier.Verify(context.Background(), "token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "turnstile siteverify returned HTTP")
		assert.NotContains(t, err.Error(), "verification failed",
			"an outage must not be reported as a failed captcha")
	}
}

func TestVerify_TransportError(t *testing.T) {
	fake := &fakeSiteverify{err: errors.New("dial tcp: connection refused")}
	verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "k"}, fake)

	err := verifier.Verify(context.Background(), "token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to verify turnstile token")
}

func TestVerify_Rejected(t *testing.T) {
	fake := &fakeSiteverify{body: `{"success": false, "error-codes": ["invalid-input-response"]}`}
	verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "k"}, fake)

	err := verifier.Verify(context.Background(), "invalid-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turnstile verification failed")
}

func TestVerify_UndecodableBody(t *testing.T) {
	fake := &fakeSiteverify{body: `{invalid-json`}
	verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "k"}, fake)

	err := verifier.Verify(context.Background(), "token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode turnstile response")
}

// TestVerify_EmptyTokenStillAsksCloudflare: the empty token is Cloudflare's
// verdict to give ("missing-input-response"), not something to short-circuit —
// short-circuiting it locally would drift from what siteverify accepts.
func TestVerify_EmptyToken(t *testing.T) {
	fake := &fakeSiteverify{body: `{"success": false, "error-codes": ["missing-input-response"]}`}
	verifier := turnstile.NewVerifier(turnstile.Options{SecretKey: "k"}, fake)

	err := verifier.Verify(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "turnstile verification failed")
	assert.Equal(t, 1, fake.calls)
}

// TestVerify_HostnameAndAction pins the two fields only WE can judge: Cloudflare
// attests that a token is genuine and unused, not that it came from our site or
// from the form we think it did.
func TestVerify_HostnameAndAction(t *testing.T) {
	tests := []struct {
		name     string
		opts     turnstile.Options
		body     string
		errorMsg string
	}{
		{
			name: "no expectations configured accepts any hostname",
			opts: turnstile.Options{SecretKey: "k"},
			body: `{"success": true, "hostname": "attacker.example", "action": "whatever"}`,
		},
		{
			name: "matching hostname and action",
			opts: turnstile.Options{SecretKey: "k", ExpectedHostname: "openmentor.io", ExpectedAction: "contact-mentor"},
			body: successBody,
		},
		{
			name: "hostname comparison is case-insensitive",
			opts: turnstile.Options{SecretKey: "k", ExpectedHostname: "OpenMentor.IO"},
			body: successBody,
		},
		{
			// A token farmed on the attacker's own page, replayed here.
			name:     "hostname mismatch",
			opts:     turnstile.Options{SecretKey: "k", ExpectedHostname: "openmentor.io"},
			body:     `{"success": true, "hostname": "attacker.example", "action": "contact-mentor"}`,
			errorMsg: "turnstile hostname mismatch",
		},
		{
			// A token harvested from a cheaper form and spent on an expensive one.
			name:     "action mismatch",
			opts:     turnstile.Options{SecretKey: "k", ExpectedAction: "register-mentor"},
			body:     successBody,
			errorMsg: "turnstile action mismatch",
		},
		{
			name:     "missing hostname in the response",
			opts:     turnstile.Options{SecretKey: "k", ExpectedHostname: "openmentor.io"},
			body:     `{"success": true}`,
			errorMsg: "turnstile hostname mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSiteverify{body: tt.body}
			err := turnstile.NewVerifier(tt.opts, fake).Verify(context.Background(), "token")
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}
