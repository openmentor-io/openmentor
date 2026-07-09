package models_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openmentor-io/openmentor-api/internal/models"
)

// mockRow implements pgx.Row interface for testing
type mockClientRequestRow struct {
	values []interface{}
	err    error
}

func (m *mockClientRequestRow) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}

	for i, v := range m.values {
		if i >= len(dest) {
			continue
		}

		switch d := dest[i].(type) {
		case *string:
			switch val := v.(type) {
			case string:
				*d = val
			case models.RequestStatus:
				*d = string(val)
			}
		case **string:
			if v == nil {
				*d = nil
			} else if str, ok := v.(string); ok {
				temp := str
				*d = &temp
			}
		case *models.RequestStatus:
			switch val := v.(type) {
			case string:
				*d = models.RequestStatus(val)
			case models.RequestStatus:
				*d = val
			}
		case *time.Time:
			if t, ok := v.(time.Time); ok {
				*d = t
			}
		case **time.Time:
			if v == nil {
				*d = nil
			} else {
				switch val := v.(type) {
				case *time.Time:
					*d = val
				case time.Time:
					temp := val
					*d = &temp
				}
			}
		}
	}

	return nil
}

// TestScanClientRequest verifies that ScanClientRequest correctly scans a PostgreSQL row
func TestScanClientRequest(t *testing.T) {
	// Prepare test data
	requestID := "650e8400-e29b-41d4-a716-446655440000"
	mentorID := "550e8400-e29b-41d4-a716-446655440000"
	email := "client@example.com"
	name := "Client Ivanov"
	telegram := "@client"
	description := "Need help with Go"
	level := "Junior"
	status := models.StatusPending
	createdAt := time.Now().AddDate(0, 0, -3)
	modifiedAt := time.Now().AddDate(0, 0, -1)
	statusChangedAt := time.Now().AddDate(0, 0, -2)
	scheduledAt := time.Now().AddDate(0, 0, 2) // 2 days in future
	declineReason := ""
	declineComment := "Some comment"
	review := "Great mentor!"

	// Create mock row
	row := &mockClientRequestRow{
		values: []interface{}{
			requestID,
			mentorID,
			email,
			name,
			telegram,
			description,
			level,
			status,
			createdAt,
			modifiedAt,
			statusChangedAt,
			&scheduledAt, // nullable *time.Time
			declineReason,
			declineComment, // nullable
			review,         // nullable, from LEFT JOIN
		},
	}

	// Scan the row
	request, err := models.ScanClientRequest(row)
	if err != nil {
		t.Fatalf("ScanClientRequest failed: %v", err)
	}

	// Verify fields
	if request.ID != requestID {
		t.Errorf("expected ID %s, got %s", requestID, request.ID)
	}

	if request.MentorID != mentorID {
		t.Errorf("expected MentorID %s, got %s", mentorID, request.MentorID)
	}

	if request.Email != email {
		t.Errorf("expected Email %s, got %s", email, request.Email)
	}

	if request.Name != name {
		t.Errorf("expected Name %s, got %s", name, request.Name)
	}

	if request.Status != models.StatusPending {
		t.Errorf("expected Status pending, got %s", request.Status)
	}

	// Verify nullable fields
	if request.ScheduledAt == nil {
		t.Error("expected ScheduledAt to be set, got nil")
	}

	if request.DeclineComment == nil || *request.DeclineComment != declineComment {
		t.Errorf("expected DeclineComment %s, got %v", declineComment, request.DeclineComment)
	}

	if request.Review == nil || *request.Review != review {
		t.Errorf("expected Review %s, got %v", review, request.Review)
	}

	// Verify computed ReviewURL
	expectedReviewURL := "https://openmentor.io/reviews/new?request_id=" + requestID
	if request.ReviewURL == nil || *request.ReviewURL != expectedReviewURL {
		t.Errorf("expected ReviewURL %s, got %v", expectedReviewURL, request.ReviewURL)
	}
}

// TestScanClientRequest_NullableFields verifies handling of null fields
func TestScanClientRequest_NullableFields(t *testing.T) {
	requestID := "650e8400-e29b-41d4-a716-446655440000"
	mentorID := "550e8400-e29b-41d4-a716-446655440000"
	createdAt := time.Now()
	modifiedAt := time.Now()
	statusChangedAt := time.Now()

	row := &mockClientRequestRow{
		values: []interface{}{
			requestID,
			mentorID,
			"test@example.com",
			"Test",
			"@test",
			"Description",
			"Junior",
			models.StatusPending,
			createdAt,
			modifiedAt,
			statusChangedAt,
			nil, // scheduled_at (null)
			"",  // decline_reason
			nil, // decline_comment (null)
			nil, // review (null)
		},
	}

	request, err := models.ScanClientRequest(row)
	if err != nil {
		t.Fatalf("ScanClientRequest failed: %v", err)
	}

	// ScheduledAt should be nil
	if request.ScheduledAt != nil {
		t.Errorf("expected ScheduledAt to be nil, got %v", *request.ScheduledAt)
	}

	// DeclineComment should be nil
	if request.DeclineComment != nil {
		t.Errorf("expected DeclineComment to be nil, got %v", *request.DeclineComment)
	}

	// Review should be nil
	if request.Review != nil {
		t.Errorf("expected Review to be nil, got %v", *request.Review)
	}

	// ReviewURL should still be computed
	if request.ReviewURL == nil {
		t.Error("expected ReviewURL to be computed even when review is nil")
	}
}

// TestScanClientRequest_DeclinedRequest verifies declined request handling
func TestScanClientRequest_DeclinedRequest(t *testing.T) {
	requestID := "650e8400-e29b-41d4-a716-446655440000"
	mentorID := "550e8400-e29b-41d4-a716-446655440000"
	declineReason := "no_time"
	declineComment := "Too busy this month"

	row := &mockClientRequestRow{
		values: []interface{}{
			requestID,
			mentorID,
			"test@example.com",
			"Test",
			"@test",
			"Description",
			"Junior",
			models.StatusDeclined,
			time.Now(),
			time.Now(),
			time.Now(),
			nil,
			declineReason,
			declineComment,
			nil,
		},
	}

	request, err := models.ScanClientRequest(row)
	if err != nil {
		t.Fatalf("ScanClientRequest failed: %v", err)
	}

	if request.Status != models.StatusDeclined {
		t.Errorf("expected Status declined, got %s", request.Status)
	}

	if request.DeclineReason != declineReason {
		t.Errorf("expected DeclineReason %s, got %s", declineReason, request.DeclineReason)
	}

	if request.DeclineComment == nil || *request.DeclineComment != declineComment {
		t.Errorf("expected DeclineComment %s, got %v", declineComment, request.DeclineComment)
	}
}

// TestScanClientRequest_Error verifies error handling
func TestScanClientRequest_Error(t *testing.T) {
	row := &mockClientRequestRow{
		err: pgx.ErrNoRows,
	}

	_, err := models.ScanClientRequest(row)
	if err != pgx.ErrNoRows {
		t.Errorf("expected pgx.ErrNoRows, got %v", err)
	}
}

// TestRequestStatusTransitions verifies status transition logic
func TestRequestStatusTransitions(t *testing.T) {
	tests := []struct {
		name        string
		from        models.RequestStatus
		to          models.RequestStatus
		shouldAllow bool
	}{
		{"pending to contacted", models.StatusPending, models.StatusContacted, true},
		{"pending to declined", models.StatusPending, models.StatusDeclined, true},
		{"pending to done", models.StatusPending, models.StatusDone, false},
		{"contacted to working", models.StatusContacted, models.StatusWorking, true},
		{"contacted to declined", models.StatusContacted, models.StatusDeclined, true},
		{"working to done", models.StatusWorking, models.StatusDone, true},
		{"working to declined", models.StatusWorking, models.StatusDeclined, true},
		{"done to any", models.StatusDone, models.StatusPending, false},
		{"declined to any", models.StatusDeclined, models.StatusWorking, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.from.CanTransitionTo(tt.to)
			if result != tt.shouldAllow {
				t.Errorf("expected CanTransitionTo(%s -> %s) = %v, got %v",
					tt.from, tt.to, tt.shouldAllow, result)
			}
		})
	}
}
