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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateMentorSlug(tt.mentorName, tt.legacyID); got != tt.want {
				t.Errorf("GenerateMentorSlug(%q, %d) = %q, want %q", tt.mentorName, tt.legacyID, got, tt.want)
			}
		})
	}
}
