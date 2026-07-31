package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// maxVisitors caps the per-IP limiter map so a burst of distinct source IPs
// can't grow it without bound between cleanup ticks. When the cap is hit,
// getVisitor first evicts idle entries; if still full it reuses a shared
// limiter for the new key (fail-safe: still rate limited, just coarser).
const maxVisitors = 50000

// RateLimiter implements a simple in-memory rate limiter per IP address
// SECURITY: Protects against abuse and DoS attacks
type RateLimiter struct {
	visitors map[string]*rate.Limiter
	mu       sync.RWMutex
	r        rate.Limit // requests per second
	b        int        // burst size
	shared   *rate.Limiter
	done     chan struct{}
}

// NewRateLimiter creates a new rate limiter
// r: requests per second (e.g., 10 means 10 requests per second)
// b: burst size (e.g., 20 means allow bursts of up to 20 requests)
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
		shared:   rate.NewLimiter(r, b),
		done:     make(chan struct{}),
	}

	// Clean up old entries every minute.
	go rl.cleanupVisitors()

	return rl
}

// Stop terminates the background cleanup goroutine. Safe to call once.
func (rl *RateLimiter) Stop() {
	close(rl.done)
}

// getVisitor returns the rate limiter for a given IP address
func (rl *RateLimiter) getVisitor(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.visitors[ip]
	if !exists {
		if len(rl.visitors) >= maxVisitors {
			rl.evictIdleLocked()
		}
		if len(rl.visitors) >= maxVisitors {
			// Still full: fall back to a shared limiter rather than growing
			// the map without bound.
			return rl.shared
		}
		limiter = rate.NewLimiter(rl.r, rl.b)
		rl.visitors[ip] = limiter
	}

	return limiter
}

// evictIdleLocked removes visitors whose limiter is back to full tokens.
// Caller must hold rl.mu.
func (rl *RateLimiter) evictIdleLocked() {
	for ip, limiter := range rl.visitors {
		if limiter.Tokens() >= float64(rl.b) {
			delete(rl.visitors, ip)
		}
	}
}

// cleanupVisitors removes inactive visitors from memory
func (rl *RateLimiter) cleanupVisitors() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.mu.Lock()
			rl.evictIdleLocked()
			rl.mu.Unlock()
		}
	}
}

// Middleware returns a Gin middleware function for rate limiting
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := rl.getVisitor(ip)

		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Allow reports whether an event for the given arbitrary key (an email, a
// token, ...) is permitted under the limit, consuming a token when it is.
// Lets callers rate-limit on something other than the client IP.
func (rl *RateLimiter) Allow(key string) bool {
	return rl.getVisitor(key).Allow()
}

// emailKeyMaxBody caps how much of the request body EmailRateLimitMiddleware
// will buffer to find the email. Login/request bodies are tiny JSON; anything
// larger is not a legitimate login request. Pair with BodySizeLimitMiddleware.
const emailKeyMaxBody = 16 << 10 // 16 KB

// EmailRateLimitMiddleware rate-limits by the lower-cased "email" field in the
// JSON body instead of the client IP.
//
// SECURITY / CORRECTNESS: the login-link endpoints sit behind the Next.js BFF,
// so every request reaches this API from the BFF's single source IP. An
// IP-keyed limiter therefore collapses into ONE global bucket and locks the
// whole platform out under load (a wave of mentors logging in at once). Keying
// on the target email both fixes that and matches the real abuse this guards
// against — email-bombing one address. The body is buffered (bounded) and
// restored so the downstream handler still reads it. A missing/blank email is
// left for the handler to reject (it never consumes a token).
func EmailRateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, emailKeyMaxBody))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}
		// Restore the body for the handler's ShouldBindJSON.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		var payload struct {
			Email string `json:"email"`
		}
		// Ignore parse errors: a malformed body isn't rate-limited here, the
		// handler will reject it. Only a well-formed email consumes a token.
		_ = json.Unmarshal(body, &payload) //nolint:errcheck // deliberate, see comment above
		key := strings.ToLower(strings.TrimSpace(payload.Email))

		if key != "" && !rl.Allow(key) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			return
		}

		c.Next()
	}
}
