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
// The size is set by the biggest body it applies to, which is the admin profile
// update (models.AdminMentorProfileUpdateRequest): 22,505 runes across its text
// fields. Runes, not bytes — go-playground/validator counts
// utf8.RuneCountInString for `max=` on a string, and JSON lets a client spend 12
// wire bytes on one rune (an astral character as a `\uXXXX\uXXXX` surrogate
// pair). So the worst body legal under every tag is 267,330 bytes, and the
// mentor's own profile save is 265,851 — both OVER the previous 256 KiB cap,
// which would have answered a legal save with a bare 413. 512 KiB carries them
// with ~1.9x to spare, and costs nothing worth counting: nothing
// admission-limits these routes, but the rate limiter in front of them bursts to
// 20, so the peak is 20 x 512 KiB = 10 MiB in a 512 MiB container.
// TestWorstCaseTextBodiesFitTheGlobalCap pins the arithmetic against the actual
// struct tags, so bumping any `max=` past the headroom fails the build.
//
// Routes needing more say so with BodySizeLimitMiddleware, which REPLACES this
// cap rather than nesting inside it (see limitedBody).
const DefaultMaxBodyBytes int64 = 512 * 1024

// MaxImageBodyBytes is the cap on the three base64-image routes (registration
// and the two picture uploads). It is not a bound on memory by itself: those
// routes are also behind the shared AdmissionLimiter, because the resident cost
// is this number times the in-flight count.
//
// It has to carry the photo the rest of the system advertises, and the photo
// travels base64-encoded inside JSON, so it is NOT the advertised photo size:
//
//	s3storage.MaxImageBytes             10,485,760  the raw file the form allows
//	base64 (4*ceil(N/3)) + data: prefix 13,981,039  what actually goes on the wire
//	the rest of the registration form      286,628  every `max=` at 12 bytes/rune
//	                                   ------------
//	                                     14,267,667  = 13.6 MiB
//
// 14 MiB is the next round number above that. At 10 MiB the largest photo that
// fit was ~7.8 MB raw, so an 8-10 MB photo passed the form's own check and
// s3storage.ValidateImage and then died at the body reader with a generic 413
// instead of the friendly PhotoRejectedError.
// TestPhotoAtTheAdvertisedLimitFitsTheImageBodyCap pins it end to end.
const MaxImageBodyBytes int64 = 14 * 1024 * 1024

// MaxUploadsInFlight is how many requests carrying MaxImageBodyBytes may be
// resident at once — the AdmissionLimiter's size, kept here because it is only
// meaningful as a multiplier of the cap above.
//
// It went 4 -> 3 when the cap went 10 -> 14 MiB, on purpose. An admitted upload
// retains roughly 3x its body (the body string, the JSON decoder's buffer, the
// base64-decoded image), and resident payloads get the same quarter of the 512
// MiB container that pkg/imageclass's decode budget gets — 128 MiB. Four slots at
// 14 MiB is 168 MiB and blows that share; three is 126 MiB, which is where 10 MiB
// x 4 already sat, and three concurrent uploads is still far above real demand (a
// handful a day). TestUploadPathFitsTheContainerMemoryBudget pins it.
const MaxUploadsInFlight = 3

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
