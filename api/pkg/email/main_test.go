package email

import (
	"os"
	"testing"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

func TestMain(m *testing.M) {
	// Use a no-op logger so package-level logging doesn't panic in tests.
	logger.Log = zap.NewNop()
	os.Exit(m.Run())
}
