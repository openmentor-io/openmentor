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
	// MentorModerationFilterDeleted is the admin-only tab of deleted profiles
	// (D70). It is not a status group like the other three — it selects on
	// deleted_at, which is why the service routes it to its own repository
	// method instead of through resolveStatuses.
	MentorModerationFilterDeleted MentorModerationFilter = "deleted"
)

func (f MentorModerationFilter) IsValid() bool {
	return f == MentorModerationFilterPending ||
		f == MentorModerationFilterApproved ||
		f == MentorModerationFilterDeclined ||
		f == MentorModerationFilterDeleted
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
	// DeletedAt is set only on rows from the admin-only "Deleted" tab (D70);
	// every status tab excludes deleted profiles, so it is nil there.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

type AdminMentorDetails struct {
	MentorID         string   `json:"mentorId"`
	LegacyID         int      `json:"id"`
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	Email            string   `json:"email"`
	PreferredContact string   `json:"contact"`
	Job              string   `json:"job"`
	Workplace        string   `json:"workplace"`
	Experience       string   `json:"experience"`
	Price            string   `json:"price"`
	Tags             []string `json:"tags"`
	About            string   `json:"about"`
	Description      string   `json:"description"`
	Competencies     string   `json:"competencies"`
	CalendarURL      string   `json:"calendarUrl"`
	Status           string   `json:"status"`
	SortOrder        int      `json:"sortOrder"`
	// ModerationNote is the reviewer note written when the profile was
	// returned to draft (cleared on approve).
	ModerationNote string `json:"moderationNote,omitempty"`
	// PhotoStyle is the auto-detected profile picture display style.
	PhotoStyle string `json:"photoStyle"`
	// ActivatedAt is set on the first approve; once set the mentor can
	// never be returned to draft.
	ActivatedAt *time.Time `json:"activatedAt,omitempty"`
	// DeletedAt marks a deleted profile (D70). Non-nil means every moderation
	// action except Restore is refused, and only an admin may see this payload
	// at all.
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	// RequestsCount is the total number of mentee requests this mentor has
	// received (all statuses) — drives the "Requests (N)" entry point.
	RequestsCount int       `json:"requestsCount"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type AdminMentorsListResponse struct {
	Mentors []AdminMentorListItem `json:"mentors"`
	Total   int                   `json:"total"`
}

type AdminMentorResponse struct {
	Mentor *AdminMentorDetails `json:"mentor"`
}

// AdminClientRequestResponse wraps a single mentee request for the admin
// moderation portal.
type AdminClientRequestResponse struct {
	Request *MentorClientRequest `json:"request"`
}

// AdminUpdateStatusRequest is the admin status override payload. Its oneof
// covers every status in AllStatuses — including 'reschedule', which the
// mentor-facing UpdateStatusRequest deliberately does not accept. Struct tags
// must be literals, so TestAdminUpdateStatusRequest_OneofCoversAllStatuses
// guards this list against drifting from AllStatuses.
type AdminUpdateStatusRequest struct {
	Status RequestStatus `json:"status" binding:"required,oneof=pending contacted working done reschedule declined unavailable"`
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
	CalendarURL      string   `json:"calendarUrl" binding:"omitempty,https_url,max=500"`
}

type AdminMentorStatusUpdateRequest struct {
	Status string `json:"status" binding:"required,oneof=active inactive"`
}

// AdminMentorReturnRequest carries the reviewer note for the 'return'
// moderation action (pending -> draft).
type AdminMentorReturnRequest struct {
	Reason string `json:"reason" binding:"required,max=2000"`
}

// AdminMentorDeleteRequest carries the typed confirmation for an admin-side
// profile deletion: the admin must retype the TARGET mentor's username, so the
// destructive action names the profile it destroys rather than relying on which
// page the click came from. Same length bound as a slug.
type AdminMentorDeleteRequest struct {
	Username string `json:"username" binding:"required,max=64"`
}

type AdminModerationTriggerPayload struct {
	Type        string `json:"type"`
	MentorID    string `json:"mentor_id"`
	Action      string `json:"action"`
	ModeratorID string `json:"moderator_id"`
	Role        string `json:"role"`
}
