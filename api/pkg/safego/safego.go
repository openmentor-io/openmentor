// Package safego runs detached background work that must never be able to
// take the process down. recover() is per-goroutine: a panic inside a bare
// `go func()` bypasses the HTTP layer's RecoveryMiddleware entirely and kills
// the whole API together with every in-flight request, so every fire-and-
// forget task (async uploads, worker triggers) goes through here.
package safego

import (
	"fmt"
	"os"
	"runtime/debug"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// Go runs fn in a new goroutine, recovering and reporting any panic. task
// names the work in the log line and the error-tracking event.
func Go(task string, fn func()) {
	go Run(task, fn)
}

// Run runs fn on the CALLING goroutine with the same recovery, for callers
// that already own one.
func Run(task string, fn func()) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		stack := debug.Stack()
		logPanic(task, recovered, stack)
		errortracking.CapturePanic(recovered, stack)
	}()
	fn()
}

// logPanic deliberately does NOT assume the global logger exists. safego is
// reached from package-level helpers that run in tests and tooling which never
// call logger.Initialize, and a nil-logger dereference from inside the
// recovery path would panic again — this time unrecoverably, which is exactly
// the failure this package exists to prevent (RecoveryMiddleware has the same
// hole).
func logPanic(task string, recovered interface{}, stack []byte) {
	if logger.Log == nil {
		fmt.Fprintf(os.Stderr, "panic in background task %q: %v\n%s\n", task, recovered, stack)
		return
	}
	logger.Error("Recovered panic in background task",
		zap.String("task", task),
		zap.Any("panic", recovered),
		zap.String("stack", string(stack)),
	)
}
