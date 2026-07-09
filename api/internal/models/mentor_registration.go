package models

// RegisterMentorRequest represents a mentor registration form submission
type RegisterMentorRequest struct {
	// Personal Info
	Name             string `json:"name" binding:"required,max=100"`
	Email            string `json:"email" binding:"required,email,max=255"`
	PreferredContact string `json:"contact" binding:"omitempty,max=100"` // Optional free-text contact details

	// Professional Info
	Job        string   `json:"job" binding:"required,max=200"`
	Workplace  string   `json:"workplace" binding:"required,max=200"`
	Experience string   `json:"experience" binding:"required,oneof=2-5 5-10 10+"`
	Price      string   `json:"price" binding:"required,max=100"`
	Tags       []string `json:"tags" binding:"required,min=1,max=5,dive,max=50"`

	// Content
	About        string `json:"about" binding:"required,max=10000"`
	Description  string `json:"description" binding:"required,max=5000"`
	Competencies string `json:"competencies" binding:"required,max=5000"`
	CalendarURL  string `json:"calendarUrl" binding:"omitempty,url,max=500"`

	// Image
	ProfilePicture ProfilePictureData `json:"profilePicture" binding:"required"`

	// Security
	CaptchaToken string `json:"captchaToken" binding:"required,min=20"`
}

// ProfilePictureData represents the profile picture upload data
type ProfilePictureData struct {
	Image       string `json:"image" binding:"required"` // base64 encoded image
	FileName    string `json:"fileName" binding:"required,max=255"`
	ContentType string `json:"contentType" binding:"required,oneof=image/jpeg image/png image/webp"`
}

// RegisterMentorResponse represents the response after registration
type RegisterMentorResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	MentorID int    `json:"mentorId,omitempty"`
	Error    string `json:"error,omitempty"`
}
