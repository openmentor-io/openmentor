package models

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RequestStatus represents the status of a client request
type RequestStatus string

const (
	StatusPending     RequestStatus = "pending"
	StatusContacted   RequestStatus = "contacted"
	StatusWorking     RequestStatus = "working"
	StatusDone        RequestStatus = "done"
	StatusDeclined    RequestStatus = "declined"
	StatusUnavailable RequestStatus = "unavailable"
)

// ActiveStatuses are statuses shown on the active requests page
var ActiveStatuses = []RequestStatus{StatusPending, StatusContacted, StatusWorking}

// PastStatuses are statuses shown on the past requests page
var PastStatuses = []RequestStatus{StatusDone, StatusDeclined, StatusUnavailable}

// IsTerminalStatus returns true if the status is terminal (no further transitions allowed)
func (s RequestStatus) IsTerminalStatus() bool {
	return s == StatusDone || s == StatusDeclined || s == StatusUnavailable
}

// CanTransitionTo checks if a status transition is valid
func (s RequestStatus) CanTransitionTo(newStatus RequestStatus) bool {
	// Terminal statuses cannot transition
	if s.IsTerminalStatus() {
		return false
	}

	switch s {
	case StatusPending:
		return newStatus == StatusContacted || newStatus == StatusDeclined
	case StatusContacted:
		return newStatus == StatusWorking || newStatus == StatusDeclined
	case StatusWorking:
		return newStatus == StatusDone || newStatus == StatusDeclined
	default:
		return false
	}
}

// DeclineReason represents predefined decline reasons
type DeclineReason string

const (
	DeclineNoTime        DeclineReason = "no_time"
	DeclineTopicMismatch DeclineReason = "topic_mismatch"
	DeclineHelpingOthers DeclineReason = "helping_others"
	DeclineOnBreak       DeclineReason = "on_break"
	DeclineOther         DeclineReason = "other"
)

// MentorClientRequest represents a mentee's request to a mentor (full admin view)
type MentorClientRequest struct {
	ID               string        `json:"id"`
	Email            string        `json:"email"`
	Name             string        `json:"name"`
	PreferredContact string        `json:"contact"`
	Details          string        `json:"details"`
	Level            string        `json:"level"`
	CreatedAt        time.Time     `json:"createdAt"`
	ModifiedAt       time.Time     `json:"modifiedAt"`
	StatusChangedAt  *time.Time    `json:"statusChangedAt"` // Nullable - may be NULL for old records
	ScheduledAt      *time.Time    `json:"scheduledAt"`
	Review           *string       `json:"review"`
	ReviewURL        *string       `json:"reviewUrl"`
	Status           RequestStatus `json:"status"`
	MentorID         string        `json:"mentorId"`
	DeclineReason    string        `json:"declineReason"`
	DeclineComment   *string       `json:"declineComment"`
}

// UpdateStatusRequest is the payload for updating request status
type UpdateStatusRequest struct {
	Status RequestStatus `json:"status" binding:"required,oneof=pending contacted working done declined unavailable"`
}

// DeclineRequestPayload is the payload for declining a request
type DeclineRequestPayload struct {
	Reason  DeclineReason `json:"reason" binding:"required,oneof=no_time topic_mismatch helping_others on_break other"`
	Comment string        `json:"comment" binding:"max=1000"`
}

// ClientRequestsResponse is the response for listing requests
type ClientRequestsResponse struct {
	Requests []MentorClientRequest `json:"requests"`
	Total    int                   `json:"total"`
}

// RequestGroup represents the type of requests to fetch
type RequestGroup string

const (
	RequestGroupActive RequestGroup = "active"
	RequestGroupPast   RequestGroup = "past"
)

// GetStatuses returns the statuses for a request group
func (g RequestGroup) GetStatuses() []RequestStatus {
	switch g {
	case RequestGroupActive:
		return ActiveStatuses
	case RequestGroupPast:
		return PastStatuses
	default:
		return nil
	}
}

// ScanClientRequest scans a single PostgreSQL row into a MentorClientRequest struct
// Expected columns: id, mentor_id, email, name, preferred_contact, description, level, status,
// created_at, updated_at, status_changed_at, scheduled_at, decline_reason, decline_comment,
// mentor_review (from LEFT JOIN reviews)
func ScanClientRequest(row pgx.Row) (*MentorClientRequest, error) {
	var r MentorClientRequest
	var scheduledAt *time.Time
	var statusChangedAt *time.Time // Allow NULL from database
	var review *string
	var declineComment *string
	var level *string         // Allow NULL from database
	var declineReason *string // Allow NULL from database

	err := row.Scan(
		&r.ID,
		&r.MentorID,
		&r.Email,
		&r.Name,
		&r.PreferredContact,
		&r.Details,
		&level, // Scan into nullable variable
		&r.Status,
		&r.CreatedAt,
		&r.ModifiedAt,
		&statusChangedAt, // Scan into nullable variable
		&scheduledAt,
		&declineReason, // Scan into nullable variable
		&declineComment,
		&review, // from LEFT JOIN reviews
	)
	if err != nil {
		return nil, err
	}

	// Set nullable fields
	if level != nil {
		r.Level = *level
	} else {
		r.Level = "" // Default to empty string for NULL values
	}
	if declineReason != nil {
		r.DeclineReason = *declineReason
	} else {
		r.DeclineReason = "" // Default to empty string for NULL values
	}
	r.StatusChangedAt = statusChangedAt
	r.ScheduledAt = scheduledAt
	r.DeclineComment = declineComment
	r.Review = review

	// Compute ReviewURL from constant base URL + request ID
	reviewURL := fmt.Sprintf("https://openmentor.io/reviews/new?request_id=%s", r.ID)
	r.ReviewURL = &reviewURL

	return &r, nil
}

// ScanClientRequests scans multiple PostgreSQL rows into a slice of MentorClientRequest structs
func ScanClientRequests(rows pgx.Rows) ([]*MentorClientRequest, error) {
	defer rows.Close()

	requests := []*MentorClientRequest{}
	for rows.Next() {
		request, err := ScanClientRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return requests, nil
}
