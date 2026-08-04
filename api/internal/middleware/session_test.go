package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
)

// Both session middlewares are the entire authorization boundary of the portal
// and the admin panel, and until H2 neither had a test. These cover the two
// questions that matter: which cookies get in, and whether a session that the
// database says is no longer good is still honored.

const (
	testSecret = "middleware-test-secret-0123456789abcdef"
	testIssuer = "openmentor-api-test"
	mentorUUID = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	modUUID    = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
)

func testTokenManager() *jwt.TokenManager {
	return jwt.NewTokenManager(testSecret, testIssuer, 24)
}

// stubChecker stands in for the repository on both sides — the mentor middleware
// asks for a status, the admin middleware for a role, and both for the session
// version.
type stubChecker struct {
	status  string
	role    string
	version int
	err     error
	calls   int
}

func (s *stubChecker) MentorSessionState(context.Context, string) (string, int, error) {
	s.calls++
	return s.status, s.version, s.err
}

func (s *stubChecker) ModeratorSessionState(context.Context, string) (string, int, error) {
	s.calls++
	return s.role, s.version, s.err
}

// run drives one request through a middleware, with the given cookie set.
func run(t *testing.T, mw gin.HandlerFunc, method, cookieName, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	reached := false
	router.Handle(method, "/probe", mw, func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(method, "/probe", http.NoBody)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK && !reached {
		t.Fatal("200 without the handler running: the test router is wrong")
	}
	if rec.Code != http.StatusOK && reached {
		t.Fatalf("handler ran despite a %d response: the middleware did not abort", rec.Code)
	}
	return rec
}

// clearsCookie reports whether the response tells the browser to drop the cookie.
// A 401 that leaves the cookie in place makes the client retry forever; a 503 that
// clears it logs everyone out over a database blip.
func clearsCookie(rec *httptest.ResponseRecorder, name string) bool {
	for _, header := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(header, name+"=;") || strings.Contains(header, name+"=; ") {
			return true
		}
	}
	return false
}

func mentorToken(t *testing.T, version int) string {
	t.Helper()
	token, err := testTokenManager().GenerateToken(mentorUUID, 1, "", "Test Mentor", version)
	if err != nil {
		t.Fatalf("mint mentor token: %v", err)
	}
	return token
}

func adminToken(t *testing.T, role string, version int) string {
	t.Helper()
	token, err := testTokenManager().GenerateTokenWithRole(modUUID, 0, "mod@example.test", "Mod", role, version)
	if err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	return token
}

func TestMentorSessionRejectsAMissingCookie(t *testing.T) {
	mw := MentorSessionMiddleware(testTokenManager(), nil, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodGet, MentorSessionCookieName, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMentorSessionAcceptsItsOwnToken(t *testing.T) {
	checker := &stubChecker{status: "active", version: 4}
	mw := MentorSessionMiddleware(testTokenManager(), checker, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodPost, MentorSessionCookieName, mentorToken(t, 4))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// TestMentorSessionRejectsAnAdminToken: both realms share a signing key, so
// without the realm binding an admin cookie pasted into mentor_session validates
// (M13). The check moved into pkg/jwt; this pins that the middleware still
// enforces it.
func TestMentorSessionRejectsAnAdminToken(t *testing.T) {
	mw := MentorSessionMiddleware(testTokenManager(), nil, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodGet, MentorSessionCookieName, adminToken(t, "admin", 1))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminSessionRejectsAMentorToken(t *testing.T) {
	mw := AdminSessionMiddleware(testTokenManager(), nil, "", false)

	rec := run(t, mw, http.MethodGet, AdminSessionCookieName, mentorToken(t, 1))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminSessionRejectsAnInvalidRole(t *testing.T) {
	mw := AdminSessionMiddleware(testTokenManager(), nil, "", false)

	rec := run(t, mw, http.MethodGet, AdminSessionCookieName, adminToken(t, "intern", 1))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestDemotingAnAdminInvalidatesTheirSession is an H2 acceptance criterion. The
// role lives in the token and the token is good for 24 hours, so without a live
// re-read a demoted (or deleted) admin kept their privileges for the rest of the
// day.
func TestDemotingAnAdminInvalidatesTheirSession(t *testing.T) {
	token := adminToken(t, "admin", 1)

	t.Run("demoted to moderator", func(t *testing.T) {
		checker := &stubChecker{role: "moderator", version: 1}
		mw := AdminSessionMiddleware(testTokenManager(), checker, "", false)

		rec := run(t, mw, http.MethodGet, AdminSessionCookieName, token)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a demoted admin", rec.Code)
		}
		if checker.calls != 1 {
			t.Errorf("checker calls = %d, want 1: the live role must be re-read on every request", checker.calls)
		}
		if !clearsCookie(rec, AdminSessionCookieName) {
			t.Error("a revoked admin session must clear the cookie")
		}
	})

	t.Run("row deleted", func(t *testing.T) {
		checker := &stubChecker{err: models.ErrSessionSubjectGone}
		mw := AdminSessionMiddleware(testTokenManager(), checker, "", false)

		rec := run(t, mw, http.MethodGet, AdminSessionCookieName, token)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a deleted moderator", rec.Code)
		}
	})

	t.Run("role unchanged still works", func(t *testing.T) {
		checker := &stubChecker{role: "admin", version: 1}
		mw := AdminSessionMiddleware(testTokenManager(), checker, "", false)

		rec := run(t, mw, http.MethodGet, AdminSessionCookieName, token)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestBumpedSessionVersionRevokesTheSession covers what logout now does: bumping
// the identity row's counter has to invalidate tokens already in the wild, which
// clearing the cookie alone never did.
func TestBumpedSessionVersionRevokesTheSession(t *testing.T) {
	t.Run("mentor", func(t *testing.T) {
		checker := &stubChecker{status: "active", version: 5}
		mw := MentorSessionMiddleware(testTokenManager(), checker, "", false, LiveCheckOnMutations)

		rec := run(t, mw, http.MethodPost, MentorSessionCookieName, mentorToken(t, 4))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a revoked mentor session", rec.Code)
		}
	})

	t.Run("admin", func(t *testing.T) {
		checker := &stubChecker{role: "admin", version: 9}
		mw := AdminSessionMiddleware(testTokenManager(), checker, "", false)

		rec := run(t, mw, http.MethodGet, AdminSessionCookieName, adminToken(t, "admin", 8))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 for a revoked admin session", rec.Code)
		}
	})
}

// TestLegacySessionsWithoutAVersionSurvive is the operational half: rejecting a
// missing session_version would log out every live session the moment this
// deploys. Delete with sessionVersionCurrent's allowance.
func TestLegacySessionsWithoutAVersionSurvive(t *testing.T) {
	checker := &stubChecker{status: "active", version: 3}
	mw := MentorSessionMiddleware(testTokenManager(), checker, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodPost, MentorSessionCookieName, mentorToken(t, 0))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a pre-D58 session: %s", rec.Code, rec.Body.String())
	}
}

// TestADeclinedMentorCannotMutate: the status check is why the re-read exists at
// all on the mentor side — a mentor declined mid-session kept write access to
// their profile for the rest of the token's life.
func TestADeclinedMentorCannotMutate(t *testing.T) {
	checker := &stubChecker{status: "declined", version: 1}
	mw := MentorSessionMiddleware(testTokenManager(), checker, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodPost, MentorSessionCookieName, mentorToken(t, 1))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a declined mentor's write", rec.Code)
	}
}

// TestOwnDataReadsSkipTheLiveCheck: under LiveCheckOnMutations a GET does not pay
// a database round trip, because the routes on that scope return only the cookie
// holder's own data — which the holder of the cookie already has.
func TestOwnDataReadsSkipTheLiveCheck(t *testing.T) {
	checker := &stubChecker{status: "declined", version: 99}
	mw := MentorSessionMiddleware(testTokenManager(), checker, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodGet, MentorSessionCookieName, mentorToken(t, 1))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: reads must not consult the checker", rec.Code)
	}
	if checker.calls != 0 {
		t.Errorf("checker calls = %d, want 0 on a GET", checker.calls)
	}
}

// TestRevokedSessionsCannotReadMenteeData is the read half of revocation, and the
// reason LiveCheckOnEveryRequest exists: the mentee inbox is the PII in this
// system, and a cookie taken off a shared machine reads it — it never needs to
// write. Scoping the re-read to mutations left that readable for the rest of the
// token's 24 hours, so each case here is a GET that must NOT be served.
func TestRevokedSessionsCannotReadMenteeData(t *testing.T) {
	cases := map[string]struct {
		checker *stubChecker
		want    int
	}{
		"logged out (session_version bumped)": {&stubChecker{status: "active", version: 5}, http.StatusUnauthorized},
		"declined mid-session":                {&stubChecker{status: "declined", version: 4}, http.StatusForbidden},
		"row deleted":                         {&stubChecker{err: models.ErrSessionSubjectGone}, http.StatusUnauthorized},
		"still good":                          {&stubChecker{status: "active", version: 4}, http.StatusOK},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mw := MentorSessionMiddleware(testTokenManager(), tc.checker, "", false, LiveCheckOnEveryRequest)

			rec := run(t, mw, http.MethodGet, MentorSessionCookieName, mentorToken(t, 4))

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.checker.calls != 1 {
				t.Errorf("checker calls = %d, want 1: the live row must be read on a PII read", tc.checker.calls)
			}
		})
	}
}

// TestPreD58SessionsStillReadMenteeData is the session-impact half of the same
// change: extending the re-read to reads must not sign anybody out. A token
// minted before session_version existed carries 0, and 0 is grandfathered on
// reads exactly as it is on writes (D56 keeps JWT_SECRET untouched, so those
// tokens are live).
func TestPreD58SessionsStillReadMenteeData(t *testing.T) {
	checker := &stubChecker{status: "active", version: 3}
	mw := MentorSessionMiddleware(testTokenManager(), checker, "", false, LiveCheckOnEveryRequest)

	rec := run(t, mw, http.MethodGet, MentorSessionCookieName, mentorToken(t, 0))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a pre-D58 session: %s", rec.Code, rec.Body.String())
	}
}

// TestACheckerFailureFailsClosedWithoutLoggingAnybodyOut: a database blip must not
// let a mutation through, and must not clear the cookie either — a 401 there would
// throw away sessions that are perfectly valid.
func TestACheckerFailureFailsClosedWithoutLoggingAnybodyOut(t *testing.T) {
	blip := errors.New("connection reset")

	t.Run("mentor", func(t *testing.T) {
		mw := MentorSessionMiddleware(testTokenManager(), &stubChecker{err: blip}, "", false, LiveCheckOnMutations)

		rec := run(t, mw, http.MethodPost, MentorSessionCookieName, mentorToken(t, 1))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		if clearsCookie(rec, MentorSessionCookieName) {
			t.Error("a database failure cleared the session cookie")
		}
	})

	t.Run("admin", func(t *testing.T) {
		mw := AdminSessionMiddleware(testTokenManager(), &stubChecker{err: blip}, "", false)

		rec := run(t, mw, http.MethodGet, AdminSessionCookieName, adminToken(t, "admin", 1))

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", rec.Code)
		}
		if clearsCookie(rec, AdminSessionCookieName) {
			t.Error("a database failure cleared the admin session cookie")
		}
	})
}

func TestSessionCookieIsClearedOnAnUnusableToken(t *testing.T) {
	mw := MentorSessionMiddleware(testTokenManager(), nil, "", false, LiveCheckOnMutations)

	rec := run(t, mw, http.MethodGet, MentorSessionCookieName, "not-a-jwt")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !clearsCookie(rec, MentorSessionCookieName) {
		t.Error("an unusable cookie must be cleared, or the client retries with it forever")
	}
}

func TestGetMentorSessionReportsAMissingOrWrongTypedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	if _, err := GetMentorSession(c); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
	c.Set(MentorSessionContextKey, "not a session")
	if _, err := GetMentorSession(c); !errors.Is(err, ErrInvalidSession) {
		t.Errorf("error = %v, want ErrInvalidSession", err)
	}

	if _, err := GetAdminSession(c); !errors.Is(err, ErrAdminSessionNotFound) {
		t.Errorf("error = %v, want ErrAdminSessionNotFound", err)
	}
	c.Set(AdminSessionContextKey, 42)
	if _, err := GetAdminSession(c); !errors.Is(err, ErrInvalidAdminSession) {
		t.Errorf("error = %v, want ErrInvalidAdminSession", err)
	}
}

func TestIsMutation(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if isMutation(method) {
			t.Errorf("isMutation(%s) = true, want false", method)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !isMutation(method) {
			t.Errorf("isMutation(%s) = false, want true", method)
		}
	}
}
