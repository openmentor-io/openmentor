package handlers

// In-package (not test/internal/handlers, where the rest of the handler tests
// live) because rotation can only be reached through the size seam, and the seam
// is deliberately unexported: production must not be able to pick a size.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFrontendLogRotates: frontend.log used to be opened O_APPEND per request
// and never rotated, so a chatty or hostile frontend filled the log volume the
// rotated files share. lumberjack has to actually roll it over.
func TestFrontendLogRotates(t *testing.T) {
	dir := t.TempDir()
	// 1 MB is lumberjack's smallest usable size and the least a test has to write.
	handler := newLogsHandler(dir, 1)

	// ~1.2 MB of entries: past the rollover, with headroom for the JSON envelope.
	entry := LogEntry{
		Timestamp: "2026-08-04T00:00:00Z",
		Level:     "info",
		Message:   strings.Repeat("x", 1024),
	}
	batch := make([]LogEntry, 128)
	for i := range batch {
		batch[i] = entry
	}
	for range 10 {
		if err := handler.writeLogsToFile(batch); err != nil {
			t.Fatalf("writeLogsToFile: %v", err)
		}
	}

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}

	var current, rotated int
	for _, name := range names {
		switch {
		case name.Name() == "frontend.log":
			current++
		case strings.HasPrefix(name.Name(), "frontend-"):
			rotated++
		}
	}

	if current != 1 {
		t.Errorf("found %d frontend.log files, want 1", current)
	}
	if rotated == 0 {
		t.Fatalf("wrote ~1.2MB past a 1MB limit and nothing rotated; files: %v", entryNames(names))
	}

	info, err := os.Stat(filepath.Join(dir, "frontend.log"))
	if err != nil {
		t.Fatalf("stat current log: %v", err)
	}
	if info.Size() > 2<<20 {
		t.Errorf("current frontend.log is %d bytes, want it bounded near the 1MB limit", info.Size())
	}
}

// The rotation settings must stay the ones pkg/logger uses for app.log and
// error.log: one rule for every file on the volume, so raising the cap in one
// place cannot leave a second file unbounded.
func TestFrontendLogRotationMatchesTheOtherLogFiles(t *testing.T) {
	if frontendLogMaxSizeMB != 100 || frontendLogMaxBackups != 7 || frontendLogMaxAgeDays != 7 {
		t.Errorf("rotation = %dMB/%d backups/%d days, want 100/7/7 (pkg/logger's settings)",
			frontendLogMaxSizeMB, frontendLogMaxBackups, frontendLogMaxAgeDays)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
