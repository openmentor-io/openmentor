package db

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/config"
)

// configureTLS sets up TLS configuration for Yandex Cloud Managed PostgreSQL.
// Returns nil if TLS is not required (local development).
//
// NOTE: certs/yandex-ca.crt (also baked into the Docker image) is only loaded
// when DATABASE_URL carries sslmode=require/verify-full, i.e. when pointing at
// Yandex Managed PostgreSQL. The self-hosted Postgres container used in
// docker-compose connects with sslmode=disable and never touches this path,
// so the cert is kept for the managed-PG deployment option.
func configureTLS(databaseURL string) (*tls.Config, error) {
	// Check if DATABASE_URL contains sslmode parameter to determine if TLS is needed
	// For local dev (localhost), typically no sslmode or sslmode=disable
	// For production, DATABASE_URL should include sslmode=verify-full or sslmode=require
	if databaseURL == "" || !containsSSLMode(databaseURL) {
		// No SSL configured - assume local development
		return nil, nil
	}

	// Load CA certificate from certs directory
	certPath := filepath.Join("certs", "yandex-ca.crt")
	caPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate from %s: %w", certPath, err)
	}

	// Create certificate pool and add CA cert
	rootCertPool := x509.NewCertPool()
	if ok := rootCertPool.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("failed to append CA certificate to pool")
	}

	// Configure TLS with CA cert
	tlsConfig := &tls.Config{
		RootCAs:    rootCertPool,
		MinVersion: tls.VersionTLS12, // Minimum TLS 1.2 for security
	}

	// Optional: Set ServerName if certificate name differs from connection hostname
	// Only needed if you get "certificate is valid for X, not Y" errors
	if serverName := os.Getenv("DATABASE_TLS_SERVER_NAME"); serverName != "" {
		tlsConfig.ServerName = serverName
	}

	return tlsConfig, nil
}

// containsSSLMode checks if DATABASE_URL has sslmode parameter
func containsSSLMode(url string) bool {
	return strings.Contains(url, "sslmode=require") ||
		strings.Contains(url, "sslmode=verify-full") ||
		strings.Contains(url, "sslmode=verify-ca")
}

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
// TLS configuration:
//   - Automatically enabled if DATABASE_URL contains sslmode=verify-full or sslmode=require
//   - Reads CA certificate from certs/yandex-ca.crt
//   - DATABASE_TLS_SERVER_NAME is optional (only needed if cert name differs from hostname)
//   - Local development (localhost without sslmode) connects without TLS
func NewPool(ctx context.Context, dbCfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	// Parse connection string and configure pool
	poolConfig, err := pgxpool.ParseConfig(dbCfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure TLS if required
	tlsConfig, err := configureTLS(dbCfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to configure TLS: %w", err)
	}
	if tlsConfig != nil {
		poolConfig.ConnConfig.TLSConfig = tlsConfig
	}

	// Configure pool settings from provided config
	poolConfig.MaxConns = dbCfg.MaxConns
	poolConfig.MinConns = dbCfg.MinConns
	poolConfig.HealthCheckPeriod = 30 * time.Second
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

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
