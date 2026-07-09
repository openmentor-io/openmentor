package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrationFilesExist verifies that migration files are present
func TestMigrationFilesExist(t *testing.T) {
	migrationsDir := "../../migrations"

	// Check if migrations directory exists
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		t.Fatalf("migrations directory does not exist: %s", migrationsDir)
	}

	// Expected migration files
	expectedFiles := []string{
		"000001_initial_schema.up.sql",
		"000001_initial_schema.down.sql",
	}

	for _, filename := range expectedFiles {
		filePath := filepath.Join(migrationsDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("migration file does not exist: %s", filePath)
		}
	}
}

// TestMigrationFilesParseable verifies that migration files contain valid SQL
func TestMigrationFilesParseable(t *testing.T) {
	migrationsDir := "../../migrations"

	migrationFiles := []string{
		"000001_initial_schema.up.sql",
		"000001_initial_schema.down.sql",
	}

	for _, filename := range migrationFiles {
		filePath := filepath.Join(migrationsDir, filename)
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read migration file %s: %v", filename, err)
		}

		// Basic validation: file should not be empty and should contain SQL keywords
		if len(content) == 0 {
			t.Errorf("migration file %s is empty", filename)
		}

		// Check for common SQL keywords to ensure it's a SQL file
		contentStr := string(content)
		if filename == "000001_initial_schema.up.sql" {
			// Up migration should contain CREATE statements
			if !contains(contentStr, "CREATE TABLE") && !contains(contentStr, "CREATE EXTENSION") {
				t.Errorf("up migration %s does not contain expected CREATE statements", filename)
			}
		} else {
			// Down migration should contain DROP statements
			if !contains(contentStr, "DROP TABLE") && !contains(contentStr, "DROP EXTENSION") {
				t.Errorf("down migration %s does not contain expected DROP statements", filename)
			}
		}
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || contains(s[1:], substr)))
}
