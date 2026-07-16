package models

// SaveProfileRequest represents a mentor profile update request
// SECURITY: Max length validation to prevent resource exhaustion attacks
type SaveProfileRequest struct {
	Name         string   `json:"name" binding:"required,max=100"`
	Job          string   `json:"job" binding:"required,max=200"`
	Workplace    string   `json:"workplace" binding:"required,max=200"`
	Experience   string   `json:"experience" binding:"required,max=50"`
	Price        string   `json:"price" binding:"required,max=100"`
	Tags         []string `json:"tags" binding:"required,max=20,dive,max=50"`
	Description  string   `json:"description" binding:"required,max=5000"`
	About        string   `json:"about" binding:"required,max=10000"`
	Competencies string   `json:"competencies" binding:"required,max=5000"`
	CalendarURL  string   `json:"calendarUrl" binding:"omitempty,url,max=500"`
}

// SaveProfileResponse represents the response after updating a profile
type SaveProfileResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// UpdateProfileStatusRequest represents a mentor's own visibility status change request
type UpdateProfileStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active inactive"`
}

// UpdateProfileStatusResponse represents the response after changing profile status
type UpdateProfileStatusResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SubmitProfileResponse represents the response after resubmitting a draft
// profile for review (POST /api/v1/mentor/profile/submit)
type SubmitProfileResponse struct {
	Success bool   `json:"success"`
	Status  string `json:"status,omitempty"`
	Error   string `json:"error,omitempty"`
}

// UploadProfilePictureRequest represents a profile picture upload request
type UploadProfilePictureRequest struct {
	Image       string `json:"image" binding:"required"`
	FileName    string `json:"fileName" binding:"required,max=255"`
	ContentType string `json:"contentType" binding:"required,oneof=image/jpeg image/png image/webp"`
}

// UploadProfilePictureResponse represents the response after uploading a profile picture
type UploadProfilePictureResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
	Error    string `json:"error,omitempty"`
}
