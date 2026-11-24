package models

import "time"

// MenteeSession is an authenticated mentee dashboard session (D95). It carries
// no email, like the mentor session: the cookie is not a place for PII.
type MenteeSession struct {
	MenteeID  string `json:"mentee_id"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

// MenteeVerifyLoginResponse is returned after a successful mentee verification.
type MenteeVerifyLoginResponse struct {
	Success bool           `json:"success"`
	Session *MenteeSession `json:"session,omitempty"`
}

// MenteeRequestGroup is the dashboard bucket a request is shown in.
type MenteeRequestGroup string

const (
	MenteeGroupPending MenteeRequestGroup = "pending"
	MenteeGroupActive  MenteeRequestGroup = "active"
	MenteeGroupDone    MenteeRequestGroup = "done"
	MenteeGroupClosed  MenteeRequestGroup = "closed"
)

// MenteeGroupFor maps a request status onto the mentee's dashboard buckets.
// 'reschedule' counts as active: the session has not happened yet. An unknown
// status lands in closed, where nothing actionable is offered.
func MenteeGroupFor(status RequestStatus) MenteeRequestGroup {
	switch status {
	case StatusPending:
		return MenteeGroupPending
	case StatusContacted, StatusWorking, StatusReschedule:
		return MenteeGroupActive
	case StatusDone:
		return MenteeGroupDone
	default:
		return MenteeGroupClosed
	}
}

// MenteeRequestMentor is the only mentor information a mentee dashboard shows.
// Deliberately no email or other contact: the mentor reaches out, and the
// calendar link is the one channel a mentor has published for booking.
type MenteeRequestMentor struct {
	Name string `json:"name"`
	// Slug is set only while the public profile page exists (an active,
	// non-deleted mentor), so the dashboard never links to a 404.
	Slug string `json:"slug,omitempty"`
	// CalendarURL is set only for pending and active requests.
	CalendarURL string `json:"calendarUrl,omitempty"`
	// Available is false once the mentor deleted their profile (D70).
	Available bool `json:"available"`
}

// MenteeReview is the review a mentee already left, shown read-only.
type MenteeReview struct {
	MentorReview   string    `json:"mentorReview"`
	PlatformReview string    `json:"platformReview"`
	Improvements   string    `json:"improvements"`
	CreatedAt      time.Time `json:"createdAt"`
}

// MenteeRequest is one request as its sender sees it.
type MenteeRequest struct {
	ID              string              `json:"id"`
	Status          RequestStatus       `json:"status"`
	Group           MenteeRequestGroup  `json:"group"`
	Details         string              `json:"details"`
	CreatedAt       time.Time           `json:"createdAt"`
	StatusChangedAt *time.Time          `json:"statusChangedAt"`
	DeclineReason   string              `json:"declineReason,omitempty"`
	DeclineComment  string              `json:"declineComment,omitempty"`
	Mentor          MenteeRequestMentor `json:"mentor"`
	Review          *MenteeReview       `json:"review"`
	CanReview       bool                `json:"canReview"`
}

// MenteeRequestsResponse lists every request sent from the session's email.
type MenteeRequestsResponse struct {
	Requests []MenteeRequest `json:"requests"`
}

// MenteeSubmitReviewRequest is a review left from the dashboard. The session is
// the authorization, so there is no review token; the request id travels in the
// body, never the path, because client_requests.id is still a legacy review
// capability while the H4 dual-read window is open.
type MenteeSubmitReviewRequest struct {
	RequestID      string `json:"requestId" binding:"required,uuid"`
	MentorReview   string `json:"mentorReview" binding:"required,min=10,max=5000"`
	PlatformReview string `json:"platformReview" binding:"max=5000"`
	Improvements   string `json:"improvements" binding:"max=5000"`
	CaptchaToken   string `json:"captchaToken" binding:"required"`
}
