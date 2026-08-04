package models

// RegisterMentorRequest represents a mentor registration form submission
type RegisterMentorRequest struct {
	// Personal Info
	Name             string `json:"name" binding:"required,max=100"`
	Email            string `json:"email" binding:"required,email,max=255"`
	PreferredContact string `json:"contact" binding:"omitempty,max=100"` // Optional free-text contact details
	// Username is the chosen profile URL part (public name for the internal
	// slug). Optional: when empty, a slug is auto-generated from the name.
	Username string `json:"username" binding:"omitempty,max=100"`

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
	CalendarURL  string `json:"calendarUrl" binding:"omitempty,https_url,max=500"`

	// Image
	ProfilePicture ProfilePictureData `json:"profilePicture" binding:"required"`

	// Security
	// max=2048 is Cloudflare's documented ceiling for a Turnstile response
	// token. Without it this field is the one unbounded string on the route, so
	// the body cap arithmetic behind middleware.MaxImageBodyBytes could not be
	// closed — the photo would be sized against a field with no size.
	CaptchaToken string `json:"captchaToken" binding:"required,min=20,max=2048"`
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
	// Reason is a machine-readable error code so the frontend can attach the
	// error to the right field ("username_taken", "username_invalid").
	Reason string `json:"reason,omitempty"`
}
