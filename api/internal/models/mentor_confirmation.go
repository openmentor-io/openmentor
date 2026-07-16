package models

import "time"

// ConfirmMentorEmailRequest is the payload of the public
// POST /api/v1/mentors/confirm and /api/v1/mentors/confirm/resend endpoints.
type ConfirmMentorEmailRequest struct {
	Token string `json:"token" binding:"required,min=10,max=200"`
}

// Error codes returned by the confirmation endpoints so the web client can
// distinguish "offer a resend" (expired) from a dead link (invalid).
const (
	ConfirmationCodeInvalid = "invalid_token"
	ConfirmationCodeExpired = "token_expired"
)

// ConfirmMentorEmailResponse is the JSON response of both confirmation
// endpoints.
type ConfirmMentorEmailResponse struct {
	Success bool   `json:"success"`
	Already bool   `json:"already,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
}

// MentorConfirmation is the minimal mentor row the confirmation flow needs,
// looked up by email_confirmation_token.
type MentorConfirmation struct {
	MentorID  string
	Name      string
	Email     string
	Status    string
	ExpiresAt time.Time
}
