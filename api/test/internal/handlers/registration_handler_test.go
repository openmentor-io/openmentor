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

// MockRegistrationService implements RegistrationServiceInterface for testing
type MockRegistrationService struct {
	mock.Mock
}

func (m *MockRegistrationService) RegisterMentor(ctx context.Context, req *models.RegisterMentorRequest) (*models.RegisterMentorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.RegisterMentorResponse), args.Error(1)
}

// TestRegistrationHandler_RegisterMentor_Success tests successful registration
func TestRegistrationHandler_RegisterMentor_Success(t *testing.T) {
	// Setup
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	// Prepare valid request
	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Senior Software Engineer",
		Workplace:    "Tech Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend", "Go", "System Design"},
		About:        "<p>Experienced backend engineer with 10+ years</p>",
		Description:  "<p>Can help with: Go, microservices, system design</p>",
		Competencies: "Go, Kubernetes, PostgreSQL, Redis",
		CalendarURL:  "https://calendly.com/johndoe",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,/9j/4AAQSkZJRg...",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "valid-recaptcha-token-12345",
	}

	// Mock successful response
	mockService.On("RegisterMentor", mock.Anything, mock.MatchedBy(func(req *models.RegisterMentorRequest) bool {
		return req.Email == "john@example.com" && req.Name == "John Doe"
	})).Return(&models.RegisterMentorResponse{
		Success:  true,
		Message:  "Registration successful",
		MentorID: 123,
	}, nil)

	// Execute request
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.RegisterMentorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 123, resp.MentorID)

	mockService.AssertExpectations(t)
}

// TestRegistrationHandler_RegisterMentor_InvalidJSON tests with malformed JSON
func TestRegistrationHandler_RegisterMentor_InvalidJSON(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	// Send invalid JSON
	req := httptest.NewRequest("POST", "/register", bytes.NewReader([]byte("{invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Contains(t, resp, "error")
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestRegistrationHandler_RegisterMentor_MissingRequiredFields tests validation
func TestRegistrationHandler_RegisterMentor_MissingRequiredFields(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	testCases := []struct {
		name        string
		requestBody models.RegisterMentorRequest
		expectError string
	}{
		{
			name: "missing_name",
			requestBody: models.RegisterMentorRequest{
				Email:        "john@example.com",
				Telegram:     "johndoe",
				Job:          "Engineer",
				Workplace:    "Company",
				Experience:   "10+",
				Price:        "$100",
				Tags:         []string{"Backend"},
				About:        "About me",
				Description:  "Description",
				Competencies: "Skills",
				ProfilePicture: models.ProfilePictureData{
					Image:       "data:image/jpeg;base64,abc",
					FileName:    "profile.jpg",
					ContentType: "image/jpeg",
				},
				RecaptchaToken: "token",
			},
			expectError: "Name",
		},
		{
			name: "missing_email",
			requestBody: models.RegisterMentorRequest{
				Name:         "John Doe",
				Telegram:     "johndoe",
				Job:          "Engineer",
				Workplace:    "Company",
				Experience:   "10+",
				Price:        "$100",
				Tags:         []string{"Backend"},
				About:        "About me",
				Description:  "Description",
				Competencies: "Skills",
				ProfilePicture: models.ProfilePictureData{
					Image:       "data:image/jpeg;base64,abc",
					FileName:    "profile.jpg",
					ContentType: "image/jpeg",
				},
				RecaptchaToken: "token",
			},
			expectError: "Email",
		},
		{
			name: "missing_tags",
			requestBody: models.RegisterMentorRequest{
				Name:         "John Doe",
				Email:        "john@example.com",
				Telegram:     "johndoe",
				Job:          "Engineer",
				Workplace:    "Company",
				Experience:   "10+",
				Price:        "$100",
				Tags:         []string{}, // Empty tags
				About:        "About me",
				Description:  "Description",
				Competencies: "Skills",
				ProfilePicture: models.ProfilePictureData{
					Image:       "data:image/jpeg;base64,abc",
					FileName:    "profile.jpg",
					ContentType: "image/jpeg",
				},
				RecaptchaToken: "token",
			},
			expectError: "Tags",
		},
		{
			name: "missing_profile_picture",
			requestBody: models.RegisterMentorRequest{
				Name:         "John Doe",
				Email:        "john@example.com",
				Telegram:     "johndoe",
				Job:          "Engineer",
				Workplace:    "Company",
				Experience:   "10+",
				Price:        "$100",
				Tags:         []string{"Backend"},
				About:        "About me",
				Description:  "Description",
				Competencies: "Skills",
				ProfilePicture: models.ProfilePictureData{
					// All fields empty - should fail validation
					Image:       "",
					FileName:    "",
					ContentType: "",
				},
				RecaptchaToken: "token",
			},
			expectError: "Image",
		},
		{
			name: "missing_recaptcha_token",
			requestBody: models.RegisterMentorRequest{
				Name:         "John Doe",
				Email:        "john@example.com",
				Telegram:     "johndoe",
				Job:          "Engineer",
				Workplace:    "Company",
				Experience:   "10+",
				Price:        "$100",
				Tags:         []string{"Backend"},
				About:        "About me",
				Description:  "Description",
				Competencies: "Skills",
				ProfilePicture: models.ProfilePictureData{
					Image:       "data:image/jpeg;base64,abc",
					FileName:    "profile.jpg",
					ContentType: "image/jpeg",
				},
			},
			expectError: "RecaptchaToken",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.requestBody)
			req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
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

// TestRegistrationHandler_RegisterMentor_InvalidEmail tests invalid email format
func TestRegistrationHandler_RegisterMentor_InvalidEmail(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "not-an-email", // Invalid format
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestRegistrationHandler_RegisterMentor_InvalidExperience tests invalid experience value
func TestRegistrationHandler_RegisterMentor_InvalidExperience(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "invalid-experience", // Invalid value
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestRegistrationHandler_RegisterMentor_TooManyTags tests tags array limit
func TestRegistrationHandler_RegisterMentor_TooManyTags(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend", "Frontend", "Mobile", "QA", "DevOps", "Security"}, // 6 tags (max is 5)
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestRegistrationHandler_RegisterMentor_TooLongFields tests field length validation
func TestRegistrationHandler_RegisterMentor_TooLongFields(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	// Name too long (> 100 chars)
	reqBody := models.RegisterMentorRequest{
		Name:         strings.Repeat("A", 101), // 101 characters
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestRegistrationHandler_RegisterMentor_InvalidContentType tests profile picture content type validation
func TestRegistrationHandler_RegisterMentor_InvalidContentType(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/gif", // Invalid content type
		},
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}

// TestRegistrationHandler_RegisterMentor_CaptchaFailed tests ReCAPTCHA failure
func TestRegistrationHandler_RegisterMentor_CaptchaFailed(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "invalid-token-12345678901234",
	}

	// Mock captcha failure
	mockService.On("RegisterMentor", mock.Anything, mock.Anything).Return(
		&models.RegisterMentorResponse{
			Success: false,
			Error:   "Captcha verification failed",
		},
		nil,
	)

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp models.RegisterMentorResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "Captcha")

	mockService.AssertExpectations(t)
}

// TestRegistrationHandler_RegisterMentor_ServiceError tests service returning error
func TestRegistrationHandler_RegisterMentor_ServiceError(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "valid-token-12345678901234",
	}

	// Mock service returning error
	mockService.On("RegisterMentor", mock.Anything, mock.Anything).Return(
		nil,
		errors.New("internal service error"),
	)

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
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

// TestRegistrationHandler_RegisterMentor_InvalidCalendarURL tests optional calendar URL validation
func TestRegistrationHandler_RegisterMentor_InvalidCalendarURL(t *testing.T) {
	mockService := new(MockRegistrationService)
	handler := handlers.NewRegistrationHandler(mockService)

	router := gin.New()
	router.POST("/register", handler.RegisterMentor)

	reqBody := models.RegisterMentorRequest{
		Name:         "John Doe",
		Email:        "john@example.com",
		Telegram:     "johndoe",
		Job:          "Engineer",
		Workplace:    "Company",
		Experience:   "10+",
		Price:        "$100",
		Tags:         []string{"Backend"},
		About:        "About me",
		Description:  "Description",
		Competencies: "Skills",
		CalendarURL:  "not-a-valid-url", // Invalid URL format
		ProfilePicture: models.ProfilePictureData{
			Image:       "data:image/jpeg;base64,abc",
			FileName:    "profile.jpg",
			ContentType: "image/jpeg",
		},
		RecaptchaToken: "token123456789012345",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Validation failed", resp["error"])
}
