package models

// MentorSession represents an authenticated mentor session
type MentorSession struct {
	LegacyID  int    `json:"legacy_id"` // Old integer ID for backwards compatibility
	MentorID  string `json:"mentor_id"` // UUID primary key
	Email     string `json:"email"`
	Name      string `json:"name"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

// RequestLoginRequest is the payload for requesting a login token
type RequestLoginRequest struct {
	Email string `json:"email" binding:"required,email,max=255"`
}

// RequestLoginResponse is returned after requesting login
type RequestLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// VerifyLoginRequest is the payload for verifying a login token
type VerifyLoginRequest struct {
	Token string `json:"token" binding:"required,min=20,max=100"`
}

// VerifyLoginResponse is returned after successful verification
type VerifyLoginResponse struct {
	Success bool           `json:"success"`
	Session *MentorSession `json:"session,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// LogoutResponse is returned after logout
type LogoutResponse struct {
	Success bool `json:"success"`
}

// MentorLoginData contains mentor data used during login
type MentorLoginData struct {
	MentorID string // UUID primary key
	LegacyID int    // Old integer ID for backwards compatibility
	Email    string
	Name     string
}
