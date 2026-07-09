package models_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openmentor-io/openmentor-api/internal/models"
)

// mockRow implements pgx.Row interface for testing
type mockRow struct {
	values []interface{}
	err    error
}

func (m *mockRow) Scan(dest ...interface{}) error {
	if m.err != nil {
		return m.err
	}

	for i, v := range m.values {
		if i >= len(dest) {
			continue
		}

		switch d := dest[i].(type) {
		case *string:
			if str, ok := v.(string); ok {
				*d = str
			}
		case **string:
			// Handle nullable string fields
			if v == nil {
				*d = nil
			} else if str, ok := v.(string); ok {
				temp := str
				*d = &temp
			}
		case *int:
			if num, ok := v.(int); ok {
				*d = num
			}
		case *int64:
			if num, ok := v.(int64); ok {
				*d = num
			}
		case **int64:
			// Handle nullable int64 fields
			if v == nil {
				*d = nil
			} else if num, ok := v.(int64); ok {
				temp := num
				*d = &temp
			}
		case *time.Time:
			if t, ok := v.(time.Time); ok {
				*d = t
			}
		}
	}

	return nil
}

// TestScanMentor verifies that ScanMentor correctly scans a PostgreSQL row
func TestScanMentor(t *testing.T) {
	// Prepare test data
	mentorID := "550e8400-e29b-41d4-a716-446655440000"
	airtableID := "rec123456" // Still in database schema, scanned but not stored in model
	legacyID := 42
	slug := "ivan-ivanov"
	name := "Ivan Ivanov"
	job := "Senior Engineer"
	workplace := "Tech Corp"
	about := "About me"
	description := "Description"
	competencies := "Go, PostgreSQL"
	experience := "5-10"
	price := "5000"
	status := "active"
	tags := "Golang,Backend,Databases" // Will be scanned as *string
	calendarURL := "https://calendly.com/ivan"
	sortOrder := 1
	createdAt := time.Now().AddDate(0, 0, -7) // 7 days ago (should be IsNew)
	updatedAt := time.Now()
	doneSessions := 4 // Count of client_requests with status='done'

	// Create mock row
	row := &mockRow{
		values: []interface{}{
			mentorID,
			airtableID, // Still in database schema, scanned but not stored in model
			legacyID,
			slug,
			name,
			job,
			workplace,
			about,
			description,
			competencies,
			experience,
			price,
			status,
			tags, // Will be scanned as *string
			calendarURL,
			sortOrder,
			createdAt,
			updatedAt,
			doneSessions, // mentee_count: aggregate of done client_requests
		},
	}

	// Scan the row
	mentor, err := models.ScanMentor(row)
	if err != nil {
		t.Fatalf("ScanMentor failed: %v", err)
	}

	// Verify fields
	if mentor.MentorID != mentorID {
		t.Errorf("expected MentorID %s, got %s", mentorID, mentor.MentorID)
	}

	if mentor.LegacyID != legacyID {
		t.Errorf("expected LegacyID %d, got %d", legacyID, mentor.LegacyID)
	}

	if mentor.Slug != slug {
		t.Errorf("expected Slug %s, got %s", slug, mentor.Slug)
	}

	if mentor.Name != name {
		t.Errorf("expected Name %s, got %s", name, mentor.Name)
	}

	// Verify computed IsVisible: status = 'active'
	if !mentor.IsVisible {
		t.Errorf("expected IsVisible to be true (status=active)")
	}

	// Verify computed IsNew: created_at > NOW() - 14 days (7 days ago should be new)
	if !mentor.IsNew {
		t.Errorf("expected IsNew to be true (created 7 days ago)")
	}

	// Verify tags parsing
	expectedTags := []string{"Golang", "Backend", "Databases"}
	if len(mentor.Tags) != len(expectedTags) {
		t.Errorf("expected %d tags, got %d", len(expectedTags), len(mentor.Tags))
	}
	for i, tag := range expectedTags {
		if i >= len(mentor.Tags) || mentor.Tags[i] != tag {
			t.Errorf("expected tag[%d] = %s, got %s", i, tag, mentor.Tags[i])
		}
	}

	// Verify calendar type
	if mentor.CalendarType != "calendly" {
		t.Errorf("expected CalendarType 'calendly', got %s", mentor.CalendarType)
	}

	// Verify sessions count (completed sessions aggregate)
	if mentor.MenteeCount != doneSessions {
		t.Errorf("expected MenteeCount %d, got %d", doneSessions, mentor.MenteeCount)
	}
	if mentor.SessionsCount != doneSessions {
		t.Errorf("expected SessionsCount %d, got %d", doneSessions, mentor.SessionsCount)
	}
}

// TestScanMentor_InactiveMentor verifies IsVisible computation for inactive mentors
func TestScanMentor_InactiveMentor(t *testing.T) {
	mentorID := "550e8400-e29b-41d4-a716-446655440000"
	createdAt := time.Now().AddDate(0, 0, -20) // 20 days ago (should NOT be IsNew)

	row := &mockRow{
		values: []interface{}{
			mentorID,      // mentor_id
			nil,           // airtable_id (null)
			1,             // legacy_id
			"test",        // slug
			"Test",        // name
			"Engineer",    // job
			"Company",     // workplace
			"About",       // about
			"Description", // description
			"Skills",      // competencies
			"0-2",         // experience
			"free",        // price
			"inactive",    // status (inactive)
			nil,           // tags (null)
			"",            // calendar_url
			0,             // sort_order
			createdAt,     // created_at
			createdAt,     // updated_at
			0,             // mentee_count (no done client_requests)
		},
	}

	mentor, err := models.ScanMentor(row)
	if err != nil {
		t.Fatalf("ScanMentor failed: %v", err)
	}

	// IsVisible should be false (status != active)
	if mentor.IsVisible {
		t.Errorf("expected IsVisible to be false for inactive mentor")
	}

	// IsNew should be false (created 20 days ago)
	if mentor.IsNew {
		t.Errorf("expected IsNew to be false for mentor created 20 days ago")
	}

	// SessionsCount should be 0 when there are no done client_requests
	if mentor.SessionsCount != 0 {
		t.Errorf("expected SessionsCount to be 0, got %d", mentor.SessionsCount)
	}
}

// TestScanMentor_Error verifies error handling
func TestScanMentor_Error(t *testing.T) {
	row := &mockRow{
		err: pgx.ErrNoRows,
	}

	_, err := models.ScanMentor(row)
	if err != pgx.ErrNoRows {
		t.Errorf("expected pgx.ErrNoRows, got %v", err)
	}
}
