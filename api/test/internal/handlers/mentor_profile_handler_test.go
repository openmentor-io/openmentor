package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/internal/handlers"
	"github.com/openmentor-io/openmentor-api/internal/middleware"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/internal/services"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func init() {
	// Initialize logger for tests (handler logs on status changes)
	_ = logger.Initialize(logger.Config{
		Level:       "info",
		Environment: "test",
		ServiceName: "openmentor-api-test",
	})
}

// MockMentorService implements MentorServiceInterface for testing
type MockMentorService struct {
	mock.Mock
}

func (m *MockMentorService) GetAllMentors(ctx context.Context, opts models.FilterOptions) ([]*models.Mentor, error) {
	args := m.Called(ctx, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Mentor), args.Error(1)
}

func (m *MockMentorService) GetMentorByID(ctx context.Context, id int, opts models.FilterOptions) (*models.Mentor, error) {
	args := m.Called(ctx, id, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Mentor), args.Error(1)
}

func (m *MockMentorService) GetMentorBySlug(ctx context.Context, slug string, opts models.FilterOptions) (*models.Mentor, error) {
	args := m.Called(ctx, slug, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Mentor), args.Error(1)
}

func (m *MockMentorService) GetMentorByMentorId(ctx context.Context, mentorId string, opts models.FilterOptions) (*models.Mentor, error) {
	args := m.Called(ctx, mentorId, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Mentor), args.Error(1)
}

// MockProfileService implements ProfileServiceInterface for testing
type MockProfileService struct {
	mock.Mock
}

func (m *MockProfileService) SaveProfileByMentorId(ctx context.Context, mentorId string, req *models.SaveProfileRequest) error {
	args := m.Called(ctx, mentorId, req)
	return args.Error(0)
}

func (m *MockProfileService) UploadPictureByMentorId(ctx context.Context, mentorId string, mentorSlug string, req *models.UploadProfilePictureRequest) (string, error) {
	args := m.Called(ctx, mentorId, mentorSlug, req)
	return args.String(0), args.Error(1)
}

func (m *MockProfileService) SetProfileStatusByMentorId(ctx context.Context, mentorId string, status string) error {
	args := m.Called(ctx, mentorId, status)
	return args.Error(0)
}

// newProfileStatusRouter builds a test router for the status endpoint.
// When session is non-nil, it is injected into the request context the same
// way MentorSessionMiddleware does after validating the session cookie.
func newProfileStatusRouter(profileService services.ProfileServiceInterface, session *models.MentorSession) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := handlers.NewMentorProfileHandler(new(MockMentorService), profileService)

	router := gin.New()
	router.POST("/profile/status", func(c *gin.Context) {
		if session != nil {
			c.Set(middleware.MentorSessionContextKey, session)
		}
		c.Next()
	}, handler.UpdateProfileStatus)
	return router
}

func performStatusRequest(router *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/profile/status", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestMentorProfileHandler_UpdateProfileStatus_ToggleToInactive(t *testing.T) {
	mockService := new(MockProfileService)
	session := &models.MentorSession{MentorID: "mentor-uuid-123", Name: "John Doe"}
	router := newProfileStatusRouter(mockService, session)

	mockService.On("SetProfileStatusByMentorId", mock.Anything, "mentor-uuid-123", "inactive").Return(nil)

	w := performStatusRequest(router, `{"status":"inactive"}`)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.UpdateProfileStatusResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "inactive", resp.Status)
	mockService.AssertExpectations(t)
}

func TestMentorProfileHandler_UpdateProfileStatus_ToggleToActive(t *testing.T) {
	mockService := new(MockProfileService)
	session := &models.MentorSession{MentorID: "mentor-uuid-123", Name: "John Doe"}
	router := newProfileStatusRouter(mockService, session)

	mockService.On("SetProfileStatusByMentorId", mock.Anything, "mentor-uuid-123", "active").Return(nil)

	w := performStatusRequest(router, `{"status":"active"}`)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.UpdateProfileStatusResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "active", resp.Status)
	mockService.AssertExpectations(t)
}

func TestMentorProfileHandler_UpdateProfileStatus_NotToggleable(t *testing.T) {
	mockService := new(MockProfileService)
	session := &models.MentorSession{MentorID: "pending-mentor-uuid", Name: "Pending Mentor"}
	router := newProfileStatusRouter(mockService, session)

	mockService.On("SetProfileStatusByMentorId", mock.Anything, "pending-mentor-uuid", "active").
		Return(services.ErrProfileStatusNotToggleable)

	w := performStatusRequest(router, `{"status":"active"}`)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Only active or inactive profiles can change visibility status", resp["error"])
	mockService.AssertExpectations(t)
}

func TestMentorProfileHandler_UpdateProfileStatus_Unauthenticated(t *testing.T) {
	mockService := new(MockProfileService)
	router := newProfileStatusRouter(mockService, nil)

	w := performStatusRequest(router, `{"status":"inactive"}`)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	mockService.AssertNotCalled(t, "SetProfileStatusByMentorId")
}

func TestMentorProfileHandler_UpdateProfileStatus_InvalidStatus(t *testing.T) {
	mockService := new(MockProfileService)
	session := &models.MentorSession{MentorID: "mentor-uuid-123", Name: "John Doe"}
	router := newProfileStatusRouter(mockService, session)

	for _, body := range []string{`{"status":"pending"}`, `{"status":"declined"}`, `{"status":""}`, `{}`, `not-json`} {
		w := performStatusRequest(router, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", body)
	}
	mockService.AssertNotCalled(t, "SetProfileStatusByMentorId")
}
