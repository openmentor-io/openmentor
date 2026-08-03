package safego

import (
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// TestRunRecoversWithUninitialisedLogger is the case that used to be fatal:
// without recovery the panic escapes and the test binary (in production, the
// API process) dies. The global logger is left nil on purpose — the recovery
// path must not need it.
func TestRunRecoversWithUninitialisedLogger(t *testing.T) {
	restore := logger.Log
	logger.Log = nil
	defer func() { logger.Log = restore }()

	ran := false
	Run("test-task", func() {
		ran = true
		panic("boom")
	})

	if !ran {
		t.Fatal("fn was not executed")
	}
}

func TestGoRecoversInDetachedGoroutine(t *testing.T) {
	restore := logger.Log
	logger.Log = zap.NewNop()
	defer func() { logger.Log = restore }()

	done := make(chan struct{})
	Go("test-task", func() {
		defer close(done)
		panic("boom")
	})

	// Reaching this line at all is the assertion: an unrecovered panic in a
	// detached goroutine takes the whole process down, test binary included.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("detached task never ran")
	}
}

func TestRunReturnsWithoutPanic(t *testing.T) {
	calls := 0
	Run("test-task", func() { calls++ })
	if calls != 1 {
		t.Fatalf("fn calls = %d, want 1", calls)
	}
}
