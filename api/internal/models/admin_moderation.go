package models

import "time"

// ModeratorRole defines moderator access levels for admin moderation area.
type ModeratorRole string

const (
	ModeratorRoleModerator ModeratorRole = "moderator"
	ModeratorRoleAdmin     ModeratorRole = "admin"
)

func (r ModeratorRole) IsValid() bool {
	return r == ModeratorRoleModerator || r == ModeratorRoleAdmin
}

// Moderator represents a moderator/admin account.
type Moderator struct {
	ID    string
	Name  string
	Email string
	Role  ModeratorRole
}

// AdminSession represents an authenticated moderator/admin web session.
type AdminSession struct {
	ModeratorID string        `json:"moderatorId"`
	Email       string        `json:"email"`
	Name        string        `json:"name"`
	Role        ModeratorRole `json:"role"`
	ExpiresAt   int64         `json:"exp"`
	IssuedAt    int64         `json:"iat"`
}

type AdminRequestLoginRequest struct {
	Email string `json:"email" binding:"required,email,max=255"`
}

type AdminRequestLoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

type AdminVerifyLoginRequest struct {
	Token string `json:"token" binding:"required,min=20,max=100"`
}

type AdminVerifyLoginResponse struct {
	Success bool          `json:"success"`
	Session *AdminSession `json:"session,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type AdminLogoutResponse struct {
	Success bool `json:"success"`
}

// MentorModerationFilter maps UI tabs to backend status groups.
type MentorModerationFilter string

const (
	MentorModerationFilterPending  MentorModerationFilter = "pending"
	MentorModerationFilterApproved MentorModerationFilter = "approved"
	MentorModerationFilterDeclined MentorModerationFilter = "declined"
)

func (f MentorModerationFilter) IsValid() bool {
	return f == MentorModerationFilterPending ||
		f == MentorModerationFilterApproved ||
		f == MentorModerationFilterDeclined
}

type AdminMentorListItem struct {
	MentorID         string    `json:"mentorId"`
	LegacyID         int       `json:"id"`
	Name             string    `json:"name"`
	Email            string    `json:"email"`
	PreferredContact string    `json:"contact"`
	Job              string    `json:"job"`
	Workplace        string    `json:"workplace"`
	Price            string    `json:"price"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
}

type AdminMentorDetails struct {
	MentorID         string    `json:"mentorId"`
	LegacyID         int       `json:"id"`
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	Email            string    `json:"email"`
	PreferredContact string    `json:"contact"`
	Job              string    `json:"job"`
	Workplace        string    `json:"workplace"`
	Experience       string    `json:"experience"`
	Price            string    `json:"price"`
	Tags             []string  `json:"tags"`
	About            string    `json:"about"`
	Description      string    `json:"description"`
	Competencies     string    `json:"competencies"`
	CalendarURL      string    `json:"calendarUrl"`
	Status           string    `json:"status"`
	SortOrder        int       `json:"sortOrder"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type AdminMentorsListResponse struct {
	Mentors []AdminMentorListItem `json:"mentors"`
	Total   int                   `json:"total"`
}

type AdminMentorResponse struct {
	Mentor *AdminMentorDetails `json:"mentor"`
}

// AdminMentorProfileUpdateRequest intentionally contains only business/profile
// fields (no secrets/login tokens).
type AdminMentorProfileUpdateRequest struct {
	Name             string   `json:"name" binding:"required,max=100"`
	Email            string   `json:"email" binding:"required,email,max=255"`
	PreferredContact string   `json:"contact" binding:"omitempty,max=100"`
	Job              string   `json:"job" binding:"required,max=200"`
	Workplace        string   `json:"workplace" binding:"required,max=200"`
	Experience       string   `json:"experience" binding:"required,max=50"`
	Price            string   `json:"price" binding:"required,max=100"`
	Tags             []string `json:"tags" binding:"required,min=1,max=20,dive,max=50"`
	Description      string   `json:"description" binding:"required,max=5000"`
	About            string   `json:"about" binding:"required,max=10000"`
	Competencies     string   `json:"competencies" binding:"required,max=5000"`
	CalendarURL      string   `json:"calendarUrl" binding:"omitempty,url,max=500"`
	Slug             *string  `json:"slug,omitempty" binding:"omitempty,max=200"`
}

type AdminMentorStatusUpdateRequest struct {
	Status string `json:"status" binding:"required,oneof=active inactive"`
}

type AdminModerationTriggerPayload struct {
	Type        string `json:"type"`
	MentorID    string `json:"mentor_id"`
	Action      string `json:"action"`
	ModeratorID string `json:"moderator_id"`
	Role        string `json:"role"`
}
