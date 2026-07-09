package models_test

import (
	"testing"

	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestGetCalendarType(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "empty URL returns none",
			url:      "",
			expected: "none",
		},
		{
			name:     "calendly URL detected",
			url:      "https://calendly.com/johndoe",
			expected: "calendly",
		},
		{
			name:     "calendly URL with uppercase",
			url:      "https://Calendly.com/johndoe",
			expected: "calendly",
		},
		{
			name:     "koalendar URL detected",
			url:      "https://koalendar.com/johndoe",
			expected: "koalendar",
		},
		{
			name:     "calendlab URL detected",
			url:      "https://calendlab.com/johndoe",
			expected: "calendlab",
		},
		{
			name:     "unknown calendar service returns url",
			url:      "https://example.com/calendar",
			expected: "url",
		},
		{
			name:     "partial match calendly",
			url:      "https://app.calendly.com/johndoe/30min",
			expected: "calendly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := models.GetCalendarType(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestAirtableRecordToMentor tests removed - Airtable conversion no longer used
// See mentor_scan_test.go for PostgreSQL row scanning tests

func TestMentorToPublicResponse(t *testing.T) {
	mentor := &models.Mentor{
		LegacyID:      1,
		Slug:          "john-doe",
		Name:          "John Doe",
		Job:           "Senior Engineer",
		Workplace:     "TechCorp",
		Description:   "Detailed description",
		About:         "About me",
		Competencies:  "React, Go, TypeScript",
		Experience:    "10 years",
		Price:         "$100/hour",
		MenteeCount:   25,
		SessionsCount: 25,
		Tags:          []string{"React", "JavaScript", "Frontend"},
		SortOrder:     1,
		IsVisible:     true,
		CalendarType:  "calendly",
		IsNew:         true,
	}

	baseURL := "https://openmentor.io"

	expected := models.PublicMentorResponse{
		ID:            1,
		Name:          "John Doe",
		Title:         "Senior Engineer",
		Workplace:     "TechCorp",
		About:         "About me",
		Description:   "Detailed description",
		Competencies:  "React, Go, TypeScript",
		Experience:    "10 years",
		Price:         "$100/hour",
		DoneSessions:  25,
		SessionsCount: 25,
		Tags:          "React,JavaScript,Frontend",
		Link:          "https://openmentor.io/mentor/john-doe",
	}

	result := mentor.ToPublicResponse(baseURL)
	assert.Equal(t, expected, result)
}

func TestMentorToPublicResponseWithEmptyTags(t *testing.T) {
	mentor := &models.Mentor{
		LegacyID:    2,
		Slug:        "jane-doe",
		Name:        "Jane Doe",
		Job:         "Engineer",
		Tags:        []string{},
		MenteeCount: 5,
	}

	baseURL := "https://openmentor.io"

	result := mentor.ToPublicResponse(baseURL)
	assert.Equal(t, "", result.Tags, "Empty tags should result in empty string")
	assert.Equal(t, "https://openmentor.io/mentor/jane-doe", result.Link)
}
