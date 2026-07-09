package db_test

import (
	"context"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/db"
)

// TestNewPool_InvalidURL verifies that pool creation fails with an invalid database URL
func TestNewPool_InvalidURL(t *testing.T) {
	ctx := context.Background()

	// Test with empty URL
	pool, err := db.NewPool(ctx, config.DatabaseConfig{
		URL:      "",
		MaxConns: 10,
		MinConns: 2,
	})
	if err == nil {
		t.Error("expected error with empty database URL, got nil")
		if pool != nil {
			pool.Close()
		}
	}

	// Test with malformed URL
	pool, err = db.NewPool(ctx, config.DatabaseConfig{
		URL:      "not-a-valid-url",
		MaxConns: 10,
		MinConns: 2,
	})
	if err == nil {
		t.Error("expected error with malformed database URL, got nil")
		if pool != nil {
			pool.Close()
		}
	}

	// Test with invalid postgres URL (wrong scheme)
	pool, err = db.NewPool(ctx, config.DatabaseConfig{
		URL:      "mysql://user:pass@localhost:3306/db",
		MaxConns: 10,
		MinConns: 2,
	})
	if err == nil {
		t.Error("expected error with non-postgres URL, got nil")
		if pool != nil {
			pool.Close()
		}
	}
}

// TestNewPool_UnreachableDatabase verifies that pool creation fails when database is unreachable
func TestNewPool_UnreachableDatabase(t *testing.T) {
	ctx := context.Background()

	// Test with unreachable database (wrong port)
	pool, err := db.NewPool(ctx, config.DatabaseConfig{
		URL:      "postgres://openmentor:password@localhost:9999/openmentor?sslmode=disable",
		MaxConns: 10,
		MinConns: 2,
	})
	if err == nil {
		t.Error("expected error with unreachable database, got nil")
		if pool != nil {
			pool.Close()
		}
	}
}

// TestClose_NilPool verifies that Close handles nil pool gracefully
func TestClose_NilPool(t *testing.T) {
	// Should not panic
	db.Close(nil)
}
