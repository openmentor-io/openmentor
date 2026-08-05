package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/internal/handlers"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
)

// The mentor route table decides which requests re-read the live mentor row, so
// the middleware's own tests cannot pin it: they would pass just as happily with
// every route registered on the loose scope. These tests drive real requests
// through registerMentorAdminRoutes.

const (
	routesSecret = "routes-test-secret-0123456789abcdef"
	routesIssuer = "openmentor-api-test"
	routesMentor = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
)

// revokedChecker answers every lookup with a session_version ahead of any token,
// i.e. "this mentor logged out"; calls counts whether the route asked at all.
type revokedChecker struct{ calls int }

func (r *revokedChecker) MentorSessionState(context.Context, string) (string, int, error) {
	r.calls++
	return "active", 99, nil
}

// menteeInboxStub stands in for the requests service. served records whether a
// request reached the handler — i.e. whether mentee data would have been sent.
type menteeInboxStub struct{ served bool }

func (m *menteeInboxStub) GetRequests(context.Context, string, string) (*models.ClientRequestsResponse, error) {
	m.served = true
	return &models.ClientRequestsResponse{}, nil
}

func (m *menteeInboxStub) GetRequestByID(context.Context, string, string) (*models.MentorClientRequest, error) {
	m.served = true
	return &models.MentorClientRequest{}, nil
}

func (m *menteeInboxStub) UpdateStatus(context.Context, string, string, models.RequestStatus) (*models.MentorClientRequest, error) {
	m.served = true
	return &models.MentorClientRequest{}, nil
}

func (m *menteeInboxStub) DeclineRequest(context.Context, string, string, *models.DeclineRequestPayload) (*models.MentorClientRequest, error) {
	m.served = true
	return &models.MentorClientRequest{}, nil
}

// mentorRouter registers the real mentor route table. The handlers other than the
// requests one are nil pointers on purpose: nothing in these tests may reach
// them, and a panic is a clearer failure than a plausible-looking 200.
func mentorRouter(checker middleware.MentorSessionChecker, inbox *menteeInboxStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	limiter := middleware.NewRateLimiter(100, 100)
	registerMentorAdminRoutes(
		router,
		&config.Config{},
		limiter, limiter,
		middleware.NewAdmissionLimiter(10, time.Second),
		nil, // mentor auth handler: GetSession never touches its receiver
		handlers.NewMentorRequestsHandler(inbox),
		nil, nil,
		jwt.NewTokenManager(routesSecret, routesIssuer, 24),
		checker,
	)
	return router
}

func revokedCookie(t *testing.T) *http.Cookie {
	t.Helper()
	token, err := jwt.NewTokenManager(routesSecret, routesIssuer, 24).
		GenerateToken(routesMentor, 1, "mentor@example.test", "Test Mentor", 1)
	if err != nil {
		t.Fatalf("mint mentor token: %v", err)
	}
	return &http.Cookie{Name: middleware.MentorSessionCookieName, Value: token}
}

// TestRevokedSessionCannotReadTheMenteeInbox: /mentor/requests returns mentee
// names, email addresses, preferred contact details and message bodies, so it is
// registered on LiveCheckOnEveryRequest. Before that, a copy of a logged-out
// mentor's cookie kept reading all of it for the rest of the token's 24 hours.
func TestRevokedSessionCannotReadTheMenteeInbox(t *testing.T) {
	for _, path := range []string{
		"/api/v1/mentor/requests?group=active",
		"/api/v1/mentor/requests/8b1f3a2c-0000-4000-8000-000000000001",
	} {
		t.Run(path, func(t *testing.T) {
			checker, inbox := &revokedChecker{}, &menteeInboxStub{}
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			req.AddCookie(revokedCookie(t))
			rec := httptest.NewRecorder()

			mentorRouter(checker, inbox).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401: %s", rec.Code, rec.Body.String())
			}
			if inbox.served {
				t.Error("mentee data was served to a revoked session")
			}
			if checker.calls != 1 {
				t.Errorf("checker calls = %d, want 1", checker.calls)
			}
		})
	}
}

// TestTheSessionPingStaysOffTheLiveCheck pins the other side of the scope split:
// the route the portal calls on every page load returns nothing but the cookie's
// own claims, so it must not add a database lookup per navigation. If this ever
// needs to flip, it is a deliberate change, not an oversight.
func TestTheSessionPingStaysOffTheLiveCheck(t *testing.T) {
	checker := &revokedChecker{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/mentor/session", http.NoBody)
	req.AddCookie(revokedCookie(t))
	rec := httptest.NewRecorder()

	mentorRouter(checker, &menteeInboxStub{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if checker.calls != 0 {
		t.Errorf("checker calls = %d, want 0 on the session ping", checker.calls)
	}
}
