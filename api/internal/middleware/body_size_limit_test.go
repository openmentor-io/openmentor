package middleware

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/imageclass"
	"github.com/openmentor-io/openmentor/api/pkg/s3storage"
)

// readAllHandler drains the body and reports whether the cap stopped it. Reading
// is the point: http.MaxBytesReader only fails at Read time, so a handler that
// never reads never notices, and a test that never reads proves nothing.
func readAllHandler(c *gin.Context) {
	if _, err := io.ReadAll(c.Request.Body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.String(http.StatusRequestEntityTooLarge, "too large")
			return
		}
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Status(http.StatusOK)
}

func postBody(t *testing.T, router *gin.Engine, path string, size int) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("a", size)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// TestGlobalCapAppliesToRoutesThatSetNone is the gap H14 names: before the global
// cap, every mentor-profile and admin-moderation POST accepted an unbounded body.
func TestGlobalCapAppliesToRoutesThatSetNone(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/profile", readAllHandler)

	assert.Equal(t, http.StatusOK, postBody(t, router, "/profile", int(DefaultMaxBodyBytes)))
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		postBody(t, router, "/profile", int(DefaultMaxBodyBytes)+1))
}

// TestRouteOverrideReplacesTheGlobalCap: the override must REPLACE, not nest.
// Nesting would leave the smaller global MaxBytesReader in the chain, so the
// image routes would still fail at the global cap and the bigger override would
// be a no-op nobody noticed until an upload failed in production.
func TestRouteOverrideReplacesTheGlobalCap(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/picture", BodySizeLimitMiddleware(MaxImageBodyBytes), readAllHandler)

	// Comfortably over the global cap, comfortably under the route's.
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(DefaultMaxBodyBytes)*4))
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(MaxImageBodyBytes)))
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		postBody(t, router, "/picture", int(MaxImageBodyBytes)+1))
}

// TestRouteOverrideCanTightenTheGlobalCap: the small hand-set caps (4 KiB on the
// username and status routes, 10 KiB on confirm) must still bind.
func TestRouteOverrideCanTightenTheGlobalCap(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/username", BodySizeLimitMiddleware(4*1024), readAllHandler)

	assert.Equal(t, http.StatusOK, postBody(t, router, "/username", 4*1024))
	assert.Equal(t, http.StatusRequestEntityTooLarge, postBody(t, router, "/username", 4*1024+1))
}

// TestRepeatedOverridesDoNotStack: three layers, the innermost wins in both
// directions.
func TestRepeatedOverridesDoNotStack(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(1024))
	router.POST("/x", BodySizeLimitMiddleware(64), BodySizeLimitMiddleware(4096), readAllHandler)

	assert.Equal(t, http.StatusOK, postBody(t, router, "/x", 4096))
	assert.Equal(t, http.StatusRequestEntityTooLarge, postBody(t, router, "/x", 4097))
}

// TestBodylessMethodsAreUntouched: the middleware must not wrap a GET, whose
// body http.Server reports as http.NoBody.
func TestBodylessMethodsAreUntouched(t *testing.T) {
	router := gin.New()
	router.Use(BodySizeLimitMiddleware(1))
	router.GET("/mentors", readAllHandler)

	req := httptest.NewRequest(http.MethodGet, "/mentors", http.NoBody)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestBodyCapAndAdmissionLimiterCoexist: the two bound different things — bytes
// per request and requests in flight — and the image routes carry both, in that
// order. Neither may swallow the other.
func TestBodyCapAndAdmissionLimiterCoexist(t *testing.T) {
	admission := NewAdmissionLimiter(1, 20*time.Millisecond)

	router := gin.New()
	router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
	router.POST("/picture", admission.Middleware(),
		BodySizeLimitMiddleware(MaxImageBodyBytes), readAllHandler)

	// The override still wins with the admission middleware between the two.
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(DefaultMaxBodyBytes)*4))
	// And the admission slot is released, so a second request is not shed.
	assert.Equal(t, http.StatusOK, postBody(t, router, "/picture", int(DefaultMaxBodyBytes)*4))
	require.Equal(t, 0, admission.InFlight())

	// An over-cap body is still rejected on size, not on admission.
	assert.Equal(t, http.StatusRequestEntityTooLarge,
		postBody(t, router, "/picture", int(MaxImageBodyBytes)+1))
	require.Equal(t, 0, admission.InFlight())
}

// bindHandler binds the body into a fresh proto and separates the two ways it
// can fail: 413 when the cap stopped the read, 400 when the payload reached the
// validator and was rejected. A worst-case body has to survive both — a payload
// the tags reject is not evidence about the cap.
func bindHandler(proto any, capture func(any)) gin.HandlerFunc {
	typ := reflect.TypeOf(proto)
	return func(c *gin.Context) {
		out := reflect.New(typ).Interface()
		if err := c.ShouldBindJSON(out); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.String(http.StatusRequestEntityTooLarge, "over the %d-byte cap", tooLarge.Limit)
				return
			}
			c.String(http.StatusBadRequest, err.Error())
			return
		}
		if capture != nil {
			capture(out)
		}
		c.Status(http.StatusOK)
	}
}

func postJSON(t *testing.T, router *gin.Engine, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestWorstCaseTextBodiesFitTheGlobalCap sizes the cap against the biggest body
// that is legal under every `max=` tag, not against the biggest body a browser
// sends. The two differ by 12x: validator counts RUNES, and a hand-written
// client can spend 12 wire bytes on one rune (astral character, `\uXXXX\uXXXX`
// surrogate pair). Counting the tags as bytes said a full profile save was ~44
// KiB with ~218 KiB to spare; it is really 265,851, and the admin variant — the
// same fields plus an email and a contact line — is 267,330. Both were over the
// old 256 KiB cap, i.e. a save that passed every validator still got a 413.
//
// The payload is generated FROM the struct tags, so bumping any `max=` past the
// headroom fails here instead of turning a legal save into a bare 413.
func TestWorstCaseTextBodiesFitTheGlobalCap(t *testing.T) {
	cases := []struct {
		name  string
		proto any
	}{
		{"mentor profile save", models.SaveProfileRequest{}},
		{"admin mentor profile update", models.AdminMentorProfileUpdateRequest{}},
		{"admin moderation return", models.AdminMentorReturnRequest{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := worstCaseJSON(t, tc.proto, nil)

			router := gin.New()
			router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
			router.POST("/save", bindHandler(tc.proto, nil))

			code, msg := postJSON(t, router, "/save", body)
			require.Equalf(t, http.StatusOK, code,
				"the worst body legal under every tag is %d bytes against the %d-byte cap: %s",
				len(body), DefaultMaxBodyBytes, msg)
		})
	}
}

// TestPhotoAtTheAdvertisedLimitFitsTheImageBodyCap is the assertion whose
// absence let three numbers disagree: the form advertises 10 MB, the server
// accepts 10 MB decoded (s3storage.MaxImageBytes) and the body reader used to
// stop at 10 MiB — but the photo travels base64 inside JSON, so a 10 MB file is
// ~13.3 MB on the wire. An 8-10 MB photo therefore passed the form's check and
// would have passed ValidateImage, and died at the body reader with a generic
// 413 instead of the friendly PhotoRejectedError.
//
// So this sends a photo of EXACTLY the advertised size, encoded the way the
// browser encodes it, alongside every other field at its worst case, and follows
// it all the way back out to the decoded bytes the size validator sees.
func TestPhotoAtTheAdvertisedLimitFitsTheImageBodyCap(t *testing.T) {
	// Content is irrelevant here — only the size is under test; the pixel bounds
	// are pkg/imageclass's job.
	photo := make([]byte, s3storage.MaxImageBytes)
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(photo)

	cases := []struct {
		name  string
		proto any
		image func(bound any) string
	}{
		{
			name:  "mentor registration",
			proto: models.RegisterMentorRequest{},
			image: func(bound any) string {
				return bound.(*models.RegisterMentorRequest).ProfilePicture.Image
			},
		},
		{
			name:  "profile picture upload",
			proto: models.UploadProfilePictureRequest{},
			image: func(bound any) string {
				return bound.(*models.UploadProfilePictureRequest).Image
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := worstCaseJSON(t, tc.proto, map[string]string{"image": dataURL})

			var bound any
			router := gin.New()
			router.Use(BodySizeLimitMiddleware(DefaultMaxBodyBytes))
			router.POST("/upload", BodySizeLimitMiddleware(MaxImageBodyBytes),
				bindHandler(tc.proto, func(v any) { bound = v }))

			code, msg := postJSON(t, router, "/upload", body)
			require.Equalf(t, http.StatusOK, code,
				"a %d-byte photo is %d bytes of request against the %d-byte cap: %s",
				s3storage.MaxImageBytes, len(body), MaxImageBodyBytes, msg)

			// And what arrived is the whole photo, not a truncated one the size
			// validator would then wave through.
			decoded, err := s3storage.DecodeImageData(tc.image(bound))
			require.NoError(t, err)
			assert.Equal(t, s3storage.MaxImageBytes, len(decoded))
			assert.NoError(t, s3storage.ValidateImageSize(decoded),
				"the body cap must not admit a photo the server then refuses, nor refuse one it would accept")
		})
	}
}

// TestUploadPathFitsTheContainerMemoryBudget is the cost of the cap above.
// MaxImageBodyBytes is per request; what the container pays is that times the
// requests allowed to be resident, plus the decode budget on top — and the
// decode budget is NOT reduced by admitting bigger bodies, so the two add.
func TestUploadPathFitsTheContainerMemoryBudget(t *testing.T) {
	// infra/docker-compose.yml: backend mem_limit 512m, GOMEMLIMIT=400MiB.
	const containerBytes int64 = 512 << 20
	const goMemLimit int64 = 400 << 20

	// An admitted upload retains its body string, the JSON decoder's buffer and
	// the base64-decoded image (0.75x the body) until it returns: ~2.75x, rounded
	// up because none of the three is freed early.
	const residentPerUpload = 3

	// Resident payloads get the same quarter of the container that
	// imageclass.DecodeBudgetBytes gets (TestDecodeBudgetFitsContainer). That is
	// the constraint the cap and the slot count trade against each other: 14 MiB
	// x 4 in flight is 168 MiB and over the share, 14 MiB x 3 is 126 MiB and just
	// inside it.
	payloads := MaxImageBodyBytes * residentPerUpload * MaxUploadsInFlight
	require.LessOrEqualf(t, payloads, containerBytes/4,
		"resident upload payloads are %d bytes (%d-byte body x%d resident x%d in flight), over the %d-byte quarter of a %d-byte container",
		payloads, MaxImageBodyBytes, residentPerUpload, MaxUploadsInFlight, containerBytes/4, containerBytes)

	// And the two halves of the upload budget add: bounding the decode does
	// nothing about the payload holding it, so they are resident together.
	peak := payloads + int64(imageclass.DecodeBudgetBytes)
	require.LessOrEqualf(t, peak, goMemLimit*3/4,
		"the upload path peaks at %d bytes (payloads plus a %d-byte decode budget) against the %d bytes of GOMEMLIMIT it may claim; the rest is the runtime, the pgx pool and every non-upload request",
		peak, int64(imageclass.DecodeBudgetBytes), goMemLimit*3/4)
}
