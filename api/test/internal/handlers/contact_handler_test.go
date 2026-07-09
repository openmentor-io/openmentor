package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/internal/handlers"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockContactService implements ContactServiceInterface for testing
type MockContactService struct {
	mock.Mock
}

func (m *MockContactService) SubmitContactForm(ctx context.Context, req *models.ContactMentorRequest) (*models.ContactMentorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ContactMentorResponse), args.Error(1)
}

// TestContactHandler_ContactMentor_Success tests successful form submission
func TestContactHandler_ContactMentor_Success(t *testing.T) {
	// Setup
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	// Prepare valid request
	reqBody := models.ContactMentorRequest{
		Email:            "test@example.com",
		Name:             "Test User",
		Experience:       "Middle",
		Intro:            "I want to learn Go programming",
		TelegramUsername: "testuser",
		MentorID:         "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken:   "valid-recaptcha-token-12345",
	}

	// Mock successful response
	mockService.On("SubmitContactForm", mock.Anything, mock.MatchedBy(func(req *models.ContactMentorRequest) bool {
		return req.Email == "test@example.com" && req.Name == "Test User"
	})).Return(&models.ContactMentorResponse{
		Success:     true,
		CalendarURL: "https://calendly.com/mentor-slug",
	}, nil)

	// Execute request
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ContactMentorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, "https://calendly.com/mentor-slug", resp.CalendarURL)

	mockService.AssertExpectations(t)
}

// TestContactHandler_ContactMentor_InvalidJSON tests with malformed JSON
func TestContactHandler_ContactMentor_InvalidJSON(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	// Send invalid JSON
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader([]byte("{invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Contains(t, resp, "error")
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestContactHandler_ContactMentor_MissingRequiredFields tests validation
func TestContactHandler_ContactMentor_MissingRequiredFields(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	testCases := []struct {
		name        string
		requestBody models.ContactMentorRequest
		expectError string
	}{
		{
			name: "missing_email",
			requestBody: models.ContactMentorRequest{
				Name:           "Test User",
				Experience:     "Middle",
				Intro:          "I want to learn",
				MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
				RecaptchaToken: "token",
			},
			expectError: "Email",
		},
		{
			name: "missing_name",
			requestBody: models.ContactMentorRequest{
				Email:          "test@example.com",
				Experience:     "Middle",
				Intro:          "I want to learn",
				MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
				RecaptchaToken: "token",
			},
			expectError: "Name",
		},
		{
			name: "missing_intro",
			requestBody: models.ContactMentorRequest{
				Email:          "test@example.com",
				Name:           "Test User",
				Experience:     "Middle",
				MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
				RecaptchaToken: "token",
			},
			expectError: "Intro",
		},
		{
			name: "missing_mentor_id",
			requestBody: models.ContactMentorRequest{
				Email:          "test@example.com",
				Name:           "Test User",
				Experience:     "Middle",
				Intro:          "I want to learn",
				RecaptchaToken: "token",
			},
			expectError: "MentorID",
		},
		{
			name: "missing_recaptcha_token",
			requestBody: models.ContactMentorRequest{
				Email:      "test@example.com",
				Name:       "Test User",
				Experience: "Middle",
				Intro:      "I want to learn",
				MentorID:   "4821fee2-7601-41ad-8798-70d57f0b2acc",
			},
			expectError: "RecaptchaToken",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)
			assert.Equal(t, "Validation failed", resp["error"])

			// Check that error details contain the expected field
			details := resp["details"].([]interface{})
			assert.NotEmpty(t, details)

			foundError := false
			for _, detail := range details {
				detailMap := detail.(map[string]interface{})
				if strings.Contains(detailMap["field"].(string), tc.expectError) {
					foundError = true
					break
				}
			}
			assert.True(t, foundError, "Expected error for field %s not found", tc.expectError)
		})
	}
}

// TestContactHandler_ContactMentor_InvalidEmail tests invalid email format
func TestContactHandler_ContactMentor_InvalidEmail(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	reqBody := models.ContactMentorRequest{
		Email:          "not-an-email", // Invalid format
		Name:           "Test User",
		Experience:     "Middle",
		Intro:          "I want to learn",
		MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestContactHandler_ContactMentor_InvalidExperience tests invalid experience value
func TestContactHandler_ContactMentor_InvalidExperience(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	reqBody := models.ContactMentorRequest{
		Email:          "test@example.com",
		Name:           "Test User",
		Experience:     "invalid-level",
		Intro:          "I want to learn",
		MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestContactHandler_ContactMentor_TooLongFields tests field length validation
func TestContactHandler_ContactMentor_TooLongFields(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	// Name too long (> 100 chars)
	reqBody := models.ContactMentorRequest{
		Email:          "test@example.com",
		Name:           strings.Repeat("A", 101), // 101 characters
		Experience:     "Middle",
		Intro:          "I want to learn",
		MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestContactHandler_ContactMentor_TooShortIntro tests minimum length validation
func TestContactHandler_ContactMentor_TooShortIntro(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	reqBody := models.ContactMentorRequest{
		Email:          "test@example.com",
		Name:           "Test User",
		Experience:     "Middle",
		Intro:          "Short", // Less than 10 characters
		MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestContactHandler_ContactMentor_CaptchaFailed tests ReCAPTCHA failure
func TestContactHandler_ContactMentor_CaptchaFailed(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	reqBody := models.ContactMentorRequest{
		Email:            "test@example.com",
		Name:             "Test User",
		Experience:       "Middle",
		Intro:            "I want to learn Go programming",
		TelegramUsername: "testuser",
		MentorID:         "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken:   "invalid-but-valid-length-token-12345", // Valid length (>= 20 chars)
	}

	// Mock captcha failure
	mockService.On("SubmitContactForm", mock.Anything, mock.Anything).Return(
		&models.ContactMentorResponse{
			Success: false,
			Error:   "Captcha verification failed",
		},
		errors.New("captcha verification failed"),
	)

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp models.ContactMentorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "Captcha")

	mockService.AssertExpectations(t)
}

// TestContactHandler_ContactMentor_ServiceError tests service returning error
func TestContactHandler_ContactMentor_ServiceError(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	reqBody := models.ContactMentorRequest{
		Email:            "test@example.com",
		Name:             "Test User",
		Experience:       "Middle",
		Intro:            "I want to learn Go programming",
		TelegramUsername: "testuser",
		MentorID:         "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken:   "valid-token-12345678901234",
	}

	// Mock service returning error
	mockService.On("SubmitContactForm", mock.Anything, mock.Anything).Return(
		nil,
		errors.New("internal service error"),
	)

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Contains(t, resp, "error")
	assert.Equal(t, "Internal server error", resp["error"])

	mockService.AssertExpectations(t)
}

// TestContactHandler_ContactMentor_WithoutTelegram verifies the telegram
// handle is optional: a request without it passes validation and succeeds.
func TestContactHandler_ContactMentor_WithoutTelegram(t *testing.T) {
	mockService := new(MockContactService)
	handler := handlers.NewContactHandler(mockService)

	router := gin.New()
	router.POST("/contact", handler.ContactMentor)

	reqBody := models.ContactMentorRequest{
		Email:      "test@example.com",
		Name:       "Test User",
		Experience: "Middle",
		Intro:      "I want to learn Go programming",
		// TelegramUsername omitted (optional)
		MentorID:       "4821fee2-7601-41ad-8798-70d57f0b2acc",
		RecaptchaToken: "valid-token-12345678901234",
	}

	mockService.On("SubmitContactForm", mock.Anything, mock.MatchedBy(func(req *models.ContactMentorRequest) bool {
		return req.Email == "test@example.com" && req.TelegramUsername == ""
	})).Return(&models.ContactMentorResponse{
		Success: true,
	}, nil)

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/contact", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.ContactMentorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)

	mockService.AssertExpectations(t)
}
