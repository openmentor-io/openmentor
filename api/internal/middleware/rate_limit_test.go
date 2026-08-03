package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// newEmailLimitedRouter wires EmailRateLimitMiddleware in front of a handler
// that echoes the body it reads — so tests can assert the body survived the
// middleware's buffering.
func newEmailLimitedRouter(rl *RateLimiter) *gin.Engine {
	r := gin.New()
	r.POST("/login", EmailRateLimitMiddleware(rl), func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, string(body))
	})
	return r
}

func post(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestEmailRateLimit_KeyedPerEmail(t *testing.T) {
	// Burst 2, no meaningful refill during the test.
	r := newEmailLimitedRouter(NewRateLimiter(0.0001, 2))

	// Two requests for the same email pass, the third is limited.
	for i := 1; i <= 2; i++ {
		if w := post(r, `{"email":"a@example.com"}`); w.Code != http.StatusOK {
			t.Fatalf("request %d for a@: want 200, got %d", i, w.Code)
		}
	}
	if w := post(r, `{"email":"a@example.com"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request for a@: want 429, got %d", w.Code)
	}

	// A DIFFERENT email is unaffected — the whole point of the fix (no shared
	// bucket across the BFF's single IP).
	if w := post(r, `{"email":"b@example.com"}`); w.Code != http.StatusOK {
		t.Fatalf("different email must not be limited by a@'s bucket, got %d", w.Code)
	}
}

func TestEmailRateLimit_NormalizesEmail(t *testing.T) {
	r := newEmailLimitedRouter(NewRateLimiter(0.0001, 1))
	if w := post(r, `{"email":"Me@Example.com "}`); w.Code != http.StatusOK {
		t.Fatalf("first: want 200, got %d", w.Code)
	}
	// Same address, different case/whitespace -> same bucket -> limited.
	if w := post(r, `{"email":"me@example.com"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("normalized duplicate: want 429, got %d", w.Code)
	}
}

func TestEmailRateLimit_BodyReachesHandler(t *testing.T) {
	r := newEmailLimitedRouter(NewRateLimiter(1, 1))
	body := `{"email":"a@example.com","extra":"kept"}`
	w := post(r, body)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if w.Body.String() != body {
		t.Fatalf("handler must receive the original body; got %q", w.Body.String())
	}
}

func TestEmailRateLimit_BlankAndMalformedPassThrough(t *testing.T) {
	// Blank/absent/malformed email never consumes a token — the handler is
	// responsible for validating it. Limiter burst 1 would 429 on the 2nd
	// token; we send many and expect all to pass through.
	r := newEmailLimitedRouter(NewRateLimiter(0.0001, 1))
	for _, body := range []string{`{"email":""}`, `{"email":"   "}`, `{}`, `not json`} {
		if w := post(r, body); w.Code != http.StatusOK {
			t.Fatalf("body %q should pass through to handler, got %d", body, w.Code)
		}
	}
}

// newTokenLimitedRouter mirrors newEmailLimitedRouter for the confirmation
// resend endpoint, which is keyed on the token rather than an email.
func newTokenLimitedRouter(rl *RateLimiter) *gin.Engine {
	r := gin.New()
	r.POST("/login", FieldRateLimitMiddleware(rl, "token"), func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, string(body))
	})
	return r
}

// TestTokenRateLimit_KeyedPerToken is the P11 lockout: the resend limiter was
// IP-keyed at 2 per 5 minutes, and behind the BFF every request shares one IP,
// so two resends by one mentor 429'd the entire platform.
func TestTokenRateLimit_KeyedPerToken(t *testing.T) {
	r := newTokenLimitedRouter(NewRateLimiter(0.0001, 2))

	for i := 1; i <= 2; i++ {
		if w := post(r, `{"token":"mcf_aaa"}`); w.Code != http.StatusOK {
			t.Fatalf("request %d for token aaa: want 200, got %d", i, w.Code)
		}
	}
	// The same token is still capped — a mentor cannot resend faster than
	// intended.
	if w := post(r, `{"token":"mcf_aaa"}`); w.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request for token aaa: want 429, got %d", w.Code)
	}
	// A DIFFERENT mentor's token has its own budget.
	if w := post(r, `{"token":"mcf_bbb"}`); w.Code != http.StatusOK {
		t.Fatalf("different token must not be limited by aaa's bucket, got %d", w.Code)
	}
}

// TestTokenRateLimit_MalformedDoesNotConsumeBudget: a payload carrying no usable
// token must reach the handler's rejection without spending anyone's budget,
// otherwise a flood of garbage locks out every real resend.
func TestTokenRateLimit_MalformedDoesNotConsumeBudget(t *testing.T) {
	r := newTokenLimitedRouter(NewRateLimiter(0.0001, 1))

	for _, body := range []string{`{}`, `not json`, `{"token":""}`, `{"token":123}`, `[]`} {
		if w := post(r, body); w.Code != http.StatusOK {
			t.Fatalf("malformed body %q must pass through to the handler, got %d", body, w.Code)
		}
	}
	// The single token in the bucket is still there for the real request.
	if w := post(r, `{"token":"mcf_aaa"}`); w.Code != http.StatusOK {
		t.Fatalf("valid request after malformed ones: want 200, got %d", w.Code)
	}
}

// TestFieldRateLimit_KeysAreFieldScoped: two limiters could share a
// RateLimiter instance, and an email must never collide with a token.
func TestFieldRateLimit_KeysAreFieldScoped(t *testing.T) {
	rl := NewRateLimiter(0.0001, 1)
	emailRouter := newEmailLimitedRouter(rl)
	tokenRouter := newTokenLimitedRouter(rl)

	if w := post(emailRouter, `{"email":"shared"}`); w.Code != http.StatusOK {
		t.Fatalf("email request: want 200, got %d", w.Code)
	}
	if w := post(tokenRouter, `{"token":"shared"}`); w.Code != http.StatusOK {
		t.Fatalf("identical token value must not share the email bucket, got %d", w.Code)
	}
}

// TestFieldRateLimit_OverlongKeyPassesThrough keeps a flood of oversized keys
// out of the per-key visitor map; the handler rejects them on length anyway.
func TestFieldRateLimit_OverlongKeyPassesThrough(t *testing.T) {
	r := newTokenLimitedRouter(NewRateLimiter(0.0001, 1))
	long := strings.Repeat("a", maxRateLimitKeyLen+1)

	for i := 1; i <= 3; i++ {
		if w := post(r, `{"token":"`+long+`"}`); w.Code != http.StatusOK {
			t.Fatalf("request %d with an over-long token: want 200, got %d", i, w.Code)
		}
	}
}
