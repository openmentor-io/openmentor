package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/handlers"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
)

// The mentee routes (D95) share a signing key with the mentor and admin realms,
// so these drive real requests through registerMenteeRoutes to pin that only a
// live mentee session reaches the mentee handlers.

type menteeVersionChecker struct{ version, calls int }

func (m *menteeVersionChecker) MenteeSessionState(context.Context, string) (int, error) {
	m.calls++
	return m.version, nil
}

type menteeServiceStub struct{ served bool }

func (m *menteeServiceStub) RequestLogin(context.Context, string) (*models.RequestLoginResponse, error) {
	return &models.RequestLoginResponse{Success: true}, nil
}
func (m *menteeServiceStub) VerifyLogin(context.Context, string) (*models.MenteeSession, string, error) {
	return nil, "", nil
}
func (m *menteeServiceStub) RevokeSession(context.Context, string) error { return nil }
func (m *menteeServiceStub) ListRequests(context.Context, string) (*models.MenteeRequestsResponse, error) {
	m.served = true
	return &models.MenteeRequestsResponse{}, nil
}
func (m *menteeServiceStub) GetSessionTTL() int      { return 3600 }
func (m *menteeServiceStub) GetCookieDomain() string { return "" }
func (m *menteeServiceStub) GetCookieSecure() bool   { return false }

func menteeRouter(checker middleware.MenteeSessionChecker, svc *menteeServiceStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	limiter := middleware.NewRateLimiter(100, 100)
	registerMenteeRoutes(router, &config.Config{}, limiter, limiter,
		handlers.NewMenteeHandler(svc, nil),
		jwt.NewTokenManager(routesSecret, routesIssuer, 24), checker)
	return router
}

func TestOnlyALiveMenteeSessionReadsTheDashboard(t *testing.T) {
	tm := jwt.NewTokenManager(routesSecret, routesIssuer, 24)
	mentee, err := tm.GenerateMenteeToken(routesMentor, 3)
	if err != nil {
		t.Fatal(err)
	}
	mentor, err := tm.GenerateToken(routesMentor, 1, "", "M", 3)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := tm.GenerateTokenWithRole(routesMentor, 0, "", "A", "admin", 3)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		cookie  *http.Cookie
		version int
		want    int
	}{
		{"live mentee session", &http.Cookie{Name: middleware.MenteeSessionCookieName, Value: mentee}, 3, http.StatusOK},
		{"revoked mentee session", &http.Cookie{Name: middleware.MenteeSessionCookieName, Value: mentee}, 4, http.StatusUnauthorized},
		{"mentor token in the mentee cookie", &http.Cookie{Name: middleware.MenteeSessionCookieName, Value: mentor}, 3, http.StatusUnauthorized},
		{"admin token in the mentee cookie", &http.Cookie{Name: middleware.MenteeSessionCookieName, Value: admin}, 3, http.StatusUnauthorized},
		{"mentor cookie only", &http.Cookie{Name: middleware.MentorSessionCookieName, Value: mentor}, 3, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &menteeServiceStub{}
			req := httptest.NewRequest(http.MethodGet, "/api/v1/mentee/requests", http.NoBody)
			req.AddCookie(tc.cookie)
			rec := httptest.NewRecorder()

			menteeRouter(&menteeVersionChecker{version: tc.version}, svc).ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if svc.served != (tc.want == http.StatusOK) {
				t.Errorf("handler served = %v", svc.served)
			}
		})
	}
}

// TestMenteeSessionPingIsLiveChecked: unlike the mentor ping, the mentee session
// route re-reads the row, so the dashboard learns at once that a logout elsewhere
// ended the session.
func TestMenteeSessionPingIsLiveChecked(t *testing.T) {
	token, err := jwt.NewTokenManager(routesSecret, routesIssuer, 24).GenerateMenteeToken(routesMentor, 1)
	if err != nil {
		t.Fatal(err)
	}
	checker := &menteeVersionChecker{version: 2}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/mentee/session", http.NoBody)
	req.AddCookie(&http.Cookie{Name: middleware.MenteeSessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	menteeRouter(checker, &menteeServiceStub{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized || checker.calls != 1 {
		t.Errorf("status = %d, checker calls = %d; want 401 after one live check", rec.Code, checker.calls)
	}
}
