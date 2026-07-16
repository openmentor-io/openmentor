package db

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/config"
)

// NewPool creates a new PostgreSQL connection pool with configuration
// Parameters:
//   - ctx: Context for the connection
//   - dbCfg: Database configuration with URL and connection limits
//
// Returns:
//   - *pgxpool.Pool: Configured connection pool
//   - error: Error if pool creation fails
//
// Connection pool configuration:
//   - MaxConns: Configurable maximum number of connections (from config)
//   - MinConns: Configurable minimum number of idle connections (from config)
//   - HealthCheckPeriod: 30s (how often to check connection health)
//   - MaxConnLifetime: 1h (maximum lifetime of a connection)
//   - MaxConnIdleTime: 30m (maximum idle time before closing)
//
// TLS configuration follows standard pgx/libpq DSN semantics: set sslmode in
// DATABASE_URL (the self-hosted compose Postgres uses sslmode=disable). For
// managed PostgreSQL with sslmode=verify-full, pass the provider CA via
// sslrootcert=<path> in DATABASE_URL or rely on the system trust store.
func NewPool(ctx context.Context, dbCfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	// Parse connection string and configure pool
	poolConfig, err := pgxpool.ParseConfig(dbCfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure pool settings from provided config
	poolConfig.MaxConns = dbCfg.MaxConns
	poolConfig.MinConns = dbCfg.MinConns
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	// Observability: pgx v5 allows a single Tracer, so fan out to both the
	// OpenTelemetry tracer (spans per query, span names trimmed to the first
	// SQL keyword to keep them bounded) and the Prometheus metrics tracer
	// (db_client_operation_duration_seconds / db_client_operation_total).
	poolConfig.ConnConfig.Tracer = NewMultiQueryTracer(
		otelpgx.NewTracer(otelpgx.WithTrimSQLInSpanName()),
		MetricsQueryTracer{},
	)

	// Create pool with config
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection by pinging database
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// Close gracefully closes the connection pool
func Close(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
	}
}
