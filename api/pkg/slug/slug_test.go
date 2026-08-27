package slug

import "testing"

func TestGenerateMentorSlug(t *testing.T) {
	tests := []struct {
		name       string
		mentorName string
		legacyID   int
		want       string
	}{
		{"simple latin name", "John Doe", 42, "john-doe-42"},
		{"cyrillic name", "Иван Петров", 123, "ivan-petrov-123"},
		{"name with special characters", "Anna-Maria O'Brien", 999, "annamaria-obrien-999"},
		{"single word name", "Cher", 1, "cher-1"},
		// Spans the stem across a hyphen but does not contain it contiguously,
		// so the D87 rule must leave it exactly as it was.
		{"name that only looks like the stem", "Tim Entorf", 3, "tim-entorf-3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateMentorSlug(tt.mentorName, tt.legacyID); got != tt.want {
				t.Errorf("GenerateMentorSlug(%q, %d) = %q, want %q", tt.mentorName, tt.legacyID, got, tt.want)
			}
		})
	}
}

// A generated slug must obey the same "mentor" rule a chosen one does —
// registration's username field is optional, so this is the path that would
// otherwise let the prohibition be bypassed by omitting one JSON field.
func TestGenerateMentorSlug_DropsMentorSegments(t *testing.T) {
	tests := []struct {
		name       string
		mentorName string
		legacyID   int
		want       string
	}{
		{"stem as surname", "Anna Mentor", 42, "anna-42"},
		{"stem as given name", "Mentor Support", 7, "support-7"},
		{"stem inside a segment", "Annamentor Ivanov", 9, "ivanov-9"},
		{"inflected stem", "Ivan Mentoring", 11, "ivan-11"},
		{"cyrillic transliterating onto the stem", "Ментор Иванов", 12, "ivanov-12"},
		{"nothing but the stem falls back", "Mentor", 5, "member-5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateMentorSlug(tt.mentorName, tt.legacyID)
			if got != tt.want {
				t.Errorf("GenerateMentorSlug(%q, %d) = %q, want %q", tt.mentorName, tt.legacyID, got, tt.want)
			}
			if isMentorDerivative(got) {
				t.Errorf("GenerateMentorSlug(%q, %d) = %q, which still reads as the stem", tt.mentorName, tt.legacyID, got)
			}
		})
	}
}
