package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/handlers"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/stretchr/testify/assert"
)

// stubAdminRequestsService records what the handler passed through so the
// routing/binding layer can be asserted without a database.
type stubAdminRequestsService struct {
	listStatus   models.RequestStatus
	gotMentorID  string
	gotRequestID string
	gotStatus    models.RequestStatus
	err          error
}

func (s *stubAdminRequestsService) ListMentorRequests(ctx context.Context, session *models.AdminSession, mentorID string, statusFilter models.RequestStatus) (*models.ClientRequestsResponse, error) {
	s.gotMentorID = mentorID
	s.listStatus = statusFilter
	if s.err != nil {
		return nil, s.err
	}
	return &models.ClientRequestsResponse{Requests: []models.MentorClientRequest{}, Total: 0}, nil
}

func (s *stubAdminRequestsService) GetMentorRequest(ctx context.Context, session *models.AdminSession, mentorID string, requestID string) (*models.MentorClientRequest, error) {
	s.gotMentorID = mentorID
	s.gotRequestID = requestID
	if s.err != nil {
		return nil, s.err
	}
	return &models.MentorClientRequest{ID: requestID, MentorID: mentorID, Status: models.StatusDone}, nil
}

func (s *stubAdminRequestsService) UpdateRequestStatus(ctx context.Context, session *models.AdminSession, mentorID string, requestID string, newStatus models.RequestStatus) (*models.MentorClientRequest, error) {
	s.gotMentorID = mentorID
	s.gotRequestID = requestID
	s.gotStatus = newStatus
	if s.err != nil {
		return nil, s.err
	}
	return &models.MentorClientRequest{ID: requestID, MentorID: mentorID, Status: newStatus}, nil
}

var _ services.AdminRequestsServiceInterface = (*stubAdminRequestsService)(nil)

// newAdminRequestsRouter mirrors the route shapes registered in cmd/api. Gin
// panics at registration time on a conflicting route tree, so building this
// router is itself the assertion that :id/:requestId nest cleanly next to the
// existing /mentors/:id/* routes.
func newAdminRequestsRouter(service services.AdminRequestsServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := handlers.NewAdminMentorRequestsHandler(service)

	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) {
		c.Set(middleware.AdminSessionContextKey, &models.AdminSession{
			ModeratorID: "mod-1",
			Role:        models.ModeratorRoleAdmin,
		})
		c.Next()
	})
	// Neighbors from cmd/api that share the /mentors/:id/ subtree.
	admin.GET("/mentors/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	admin.GET("/mentors/:id/username/availability", func(c *gin.Context) { c.Status(http.StatusOK) })
	admin.GET("/mentors/:id/requests", handler.ListMentorRequests)
	admin.GET("/mentors/:id/requests/:requestId", handler.GetMentorRequest)
	admin.POST("/mentors/:id/requests/:requestId/status", handler.UpdateMentorRequestStatus)

	return router
}

func TestAdminMentorRequestsHandler_ListRoutesAndStatusFilter(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantStatus int
		wantFilter models.RequestStatus
	}{
		{"no filter", "/api/v1/admin/mentors/mentor-1/requests", http.StatusOK, ""},
		{"valid filter", "/api/v1/admin/mentors/mentor-1/requests?status=declined", http.StatusOK, models.StatusDeclined},
		{"invalid filter", "/api/v1/admin/mentors/mentor-1/requests?status=archived", http.StatusBadRequest, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &stubAdminRequestsService{}
			router := newAdminRequestsRouter(service)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.url, http.NoBody)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, "mentor-1", service.gotMentorID)
				assert.Equal(t, tt.wantFilter, service.listStatus)
			}
		})
	}
}

func TestAdminMentorRequestsHandler_GetRequestRoute(t *testing.T) {
	service := &stubAdminRequestsService{}
	router := newAdminRequestsRouter(service)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/mentors/mentor-1/requests/req-9", http.NoBody)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "mentor-1", service.gotMentorID)
	assert.Equal(t, "req-9", service.gotRequestID)

	var body models.AdminClientRequestResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "req-9", body.Request.ID)
}

// Terminal statuses must bind here — the admin override is what un-closes a
// request that was closed by mistake.
func TestAdminMentorRequestsHandler_UpdateStatusAcceptsEveryStatus(t *testing.T) {
	for _, status := range models.AllStatuses {
		t.Run(string(status), func(t *testing.T) {
			service := &stubAdminRequestsService{}
			router := newAdminRequestsRouter(service)

			payload, err := json.Marshal(map[string]string{"status": string(status)})
			assert.NoError(t, err)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/mentors/mentor-1/requests/req-9/status", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, status, service.gotStatus)
		})
	}
}

func TestAdminMentorRequestsHandler_UpdateStatusRejectsUnknownStatus(t *testing.T) {
	service := &stubAdminRequestsService{}
	router := newAdminRequestsRouter(service)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/mentors/mentor-1/requests/req-9/status", bytes.NewReader([]byte(`{"status":"archived"}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, models.RequestStatus(""), service.gotStatus)
}

func TestAdminMentorRequestsHandler_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"forbidden for moderators", services.ErrAdminForbiddenAction, http.StatusForbidden},
		{"not found", services.ErrRequestNotFound, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &stubAdminRequestsService{err: tt.err}
			router := newAdminRequestsRouter(service)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/mentors/mentor-1/requests/req-9", http.NoBody)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}
