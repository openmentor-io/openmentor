package middleware

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// DefaultMaxBodyBytes is the cap applied to EVERY route by the global
// middleware, so a route added without thinking about payload size is bounded
// rather than unbounded — today that is the whole mentor-profile and admin
// moderation surface, which sets no cap at all.
//
// It is deliberately small: the largest hand-set cap outside the image endpoints
// is 100 KiB (contact, reviews), and the biggest legitimate body under it is a
// full profile save — a few 5,000-char text fields plus tags.
//
// Routes needing more say so with BodySizeLimitMiddleware, which REPLACES this
// cap rather than nesting inside it (see limitedBody).
const DefaultMaxBodyBytes int64 = 256 * 1024

// MaxImageBodyBytes is the cap on the three base64-image routes (registration
// and the two picture uploads). It is not a bound on memory by itself: those
// routes are also behind the shared AdmissionLimiter, because the resident cost
// is this number times the in-flight count.
const MaxImageBodyBytes int64 = 10 * 1024 * 1024

// limitedBody remembers the reader a cap was applied to, so a later, deliberate
// cap starts from the ORIGINAL body instead of stacking on top of a smaller one.
//
// Stacking is the trap this exists to avoid: with a global cap installed,
// wrapping again would leave the smaller outer MaxBytesReader in the chain, so
// the 10 MiB image routes would still fail at the global limit and the override
// would silently do nothing.
type limitedBody struct {
	io.ReadCloser
	original io.ReadCloser
}

// BodySizeLimitMiddleware limits the size of request bodies.
// SECURITY: Prevents denial-of-service attacks through oversized payloads.
//
// Applied globally with DefaultMaxBodyBytes and per-route with an explicit
// value; the per-route value wins in both directions.
func BodySizeLimitMiddleware(maxBodySize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip for GET, HEAD, OPTIONS requests (no body)
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		if c.Request.Body != nil {
			original := c.Request.Body
			if already, ok := original.(*limitedBody); ok {
				original = already.original
			}
			c.Request.Body = &limitedBody{
				ReadCloser: http.MaxBytesReader(c.Writer, original, maxBodySize),
				original:   original,
			}
		}

		c.Next()
	}
}
