package main

import (
	"fmt"
	"os"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/db"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	err = logger.Initialize(logger.Config{
		Level:       cfg.Logging.Level,
		LogDir:      cfg.Logging.Dir,
		ServiceName: "openmentor-migrate",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting database migrations",
		zap.String("database", maskDatabaseURL(cfg.Database.URL)))

	// Run migrations
	if err := db.RunMigrations(cfg.Database.URL, "file://migrations"); err != nil {
		logger.Error("Failed to run migrations", zap.Error(err))
		logger.Sync() //nolint:errcheck // Best effort sync before exit
		os.Exit(1)    //nolint:gocritic // Manually synced logger above
	}

	logger.Info("Database migrations completed successfully")
}

// maskDatabaseURL masks the password in database URL for logging
func maskDatabaseURL(url string) string {
	// Simple masking - just show we're connecting without revealing password
	if len(url) > 20 {
		return url[:20] + "***"
	}
	return "***"
}
