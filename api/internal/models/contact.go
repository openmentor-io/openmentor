package models

// ContactMentorRequest represents a contact form submission
type ContactMentorRequest struct {
	Name             string `json:"name" binding:"required,min=2,max=100"`
	Email            string `json:"email" binding:"required,email,max=255"`
	Experience       string `json:"experience" binding:"omitempty,oneof=Junior Middle Senior Manager 'Manager of managers' C-level"`
	MentorID         string `json:"mentorId" binding:"required,uuid"`
	Intro            string `json:"intro" binding:"required,min=10,max=4000"`
	TelegramUsername string `json:"telegramUsername" binding:"omitempty,max=50"` // Optional contact handle
	RecaptchaToken   string `json:"recaptchaToken" binding:"required,min=20"`
}

// ContactMentorResponse represents the response after submitting a contact form
type ContactMentorResponse struct {
	Success     bool   `json:"success"`
	RequestID   string `json:"requestId,omitempty"`
	CalendarURL string `json:"calendar_url,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ClientRequest represents a client request record
type ClientRequest struct {
	Email       string
	Name        string
	Level       string
	MentorID    string // Mentor UUID
	Description string
	Telegram    string
}

// ReCAPTCHAResponse represents Google's ReCAPTCHA verification response
type ReCAPTCHAResponse struct {
	Success     bool     `json:"success"`
	ChallengeTS string   `json:"challenge_ts"`
	Hostname    string   `json:"hostname"`
	ErrorCodes  []string `json:"error-codes"`
}
