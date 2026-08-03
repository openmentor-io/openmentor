package safego

import (
	"os"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

func TestMain(m *testing.M) {
	// Use a no-op logger so the recovery path logs somewhere harmless.
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}

func TestRunReturnsWithoutPanic(t *testing.T) {
	calls := 0
	Run("test-task", func() { calls++ })
	if calls != 1 {
		t.Fatalf("fn calls = %d, want 1", calls)
	}
}

// TestRunRecoversWithUninitialisedLogger is the case that used to be fatal
// twice over: the panic escapes, and the recovery path itself nil-derefs the
// global logger (RecoveryMiddleware has that second hole). Nothing else may run
// concurrently here — the global is mutated — which the channel handoff in
// TestGoRecoversInDetachedGoroutine guarantees.
func TestRunRecoversWithUninitialisedLogger(t *testing.T) {
	logger.Log = nil
	defer func() { logger.Log = zap.NewNop() }()

	ran := false
	Run("test-task", func() {
		ran = true
		panic("boom")
	})

	if !ran {
		t.Fatal("fn was not executed")
	}
}

// TestGoRecoversInDetachedGoroutine: a panic in a bare `go func()` takes the
// whole process down, test binary included, because recover() is per-goroutine
// and the caller has already returned. Reaching the close() — which sits AFTER
// the recovering call, so it also orders the recovery before this test ends —
// is the assertion.
func TestGoRecoversInDetachedGoroutine(t *testing.T) {
	done := make(chan struct{})
	Go("outer-task", func() {
		Run("inner-task", func() { panic("boom") })
		close(done)
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the panicking task never returned: it was not recovered")
	}
}
