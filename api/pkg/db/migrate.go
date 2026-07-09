package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file" // Register file source driver
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// RunMigrations executes database migrations from the specified path
// Parameters:
//   - databaseURL: PostgreSQL connection string
//   - migrationsPath: Path to migration files (e.g., "file://./migrations")
//
// Returns error if migrations fail, ignores ErrNoChange (already up to date)
func RunMigrations(databaseURL, migrationsPath string) error {
	// Parse connection config from URL
	connConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure TLS using the same CA cert as the main connection pool
	tlsConfig, err := configureTLS(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to configure TLS: %w", err)
	}
	if tlsConfig != nil {
		connConfig.TLSConfig = tlsConfig
	}

	// Open database connection via pgx stdlib adapter
	db := stdlib.OpenDB(*connConfig)
	defer db.Close()

	// Ping database to verify connection
	if pingErr := db.Ping(); pingErr != nil {
		return fmt.Errorf("failed to ping database: %w", pingErr)
	}

	// Create postgres driver instance for migrations
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	// Create migrate instance with file source and postgres driver
	m, err := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Run all pending migrations
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
