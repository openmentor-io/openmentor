package main

import (
	"fmt"
	"os"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/db"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/redact"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.LoadFor(config.BinaryMigrate)
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

	// redact.DSN, not a byte prefix: the mask this replaced was
	// `url[:20] + "***"`, which keeps a fixed 20-byte window and starts leaking
	// password bytes the moment the username drops below 8 characters (C11).
	// The user survives on purpose — since H8 it names WHICH role connected
	// (om_migrate vs the superuser fallback), which is the first thing to check
	// when a migration is refused.
	logger.Info("Starting database migrations",
		zap.String("database", redact.DSN(cfg.Database.URL)))

	// Run migrations
	if err := db.RunMigrations(cfg.Database.URL, "file://migrations"); err != nil {
		// pgx's ParseConfigError renders the DSN it was handed back into its
		// message, so this line needs the redacting form as much as the one above.
		logger.Error("Failed to run migrations", logger.RedactedError(err))
		logger.Sync() //nolint:errcheck // Best effort sync before exit
		os.Exit(1)    //nolint:gocritic // Manually synced logger above
	}

	logger.Info("Database migrations completed successfully")
}
