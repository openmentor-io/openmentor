package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryOperation(t *testing.T) {
	tests := []struct {
		sql  string
		want string
	}{
		{"SELECT * FROM mentors", "select"},
		{"select 1", "select"},
		{"  \n\tSELECT id FROM mentors", "select"},
		{"INSERT INTO mentors (name) VALUES ($1)", "insert"},
		{"UPDATE mentors SET name = $1", "update"},
		{"DELETE FROM mentors WHERE id = $1", "delete"},
		{"BEGIN", "begin"},
		{"begin isolation level serializable", "begin"},
		{"COMMIT", "commit"},
		{"ROLLBACK", "other"},
		{"TRUNCATE mentors", "other"},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", "other"},
		{"EXPLAIN SELECT 1", "other"},
		{"", "other"},
		{"   ", "other"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, queryOperation(tt.sql), "sql: %q", tt.sql)
	}
}

func TestMetricsQueryTracer_RecordsMetrics(t *testing.T) {
	metrics.Init("db-tracer-test")

	tracer := MetricsQueryTracer{}

	runQuery := func(sql string, queryErr error) {
		ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql})
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: queryErr})
	}

	runQuery("SELECT * FROM mentors", nil)
	runQuery("select 1", nil)
	runQuery("INSERT INTO mentors (name) VALUES ($1)", errors.New("boom"))
	runQuery("TRUNCATE mentors", nil)

	assert.Equal(t, float64(2), testutil.ToFloat64(metrics.DBRequestTotal.WithLabelValues("select", "success")))
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.DBRequestTotal.WithLabelValues("insert", "error")))
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.DBRequestTotal.WithLabelValues("other", "success")))
	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.DBRequestTotal.WithLabelValues("insert", "success")))

	// Histogram observations recorded for the same three label sets:
	// (select, success), (insert, error), (other, success).
	assert.Equal(t, 3, testutil.CollectAndCount(metrics.DBRequestDuration))
}

func TestMetricsQueryTracer_EndWithoutStartIsNoop(t *testing.T) {
	metrics.Init("db-tracer-test-noop")

	tracer := MetricsQueryTracer{}

	// No panic and no recording when the context carries no start data.
	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})

	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.DBRequestTotal.WithLabelValues("other", "success")))
}

// recordingTracer captures TraceQueryStart/End invocations and stamps the
// context so chaining across the multi-tracer can be asserted.
type recordingTracer struct {
	name       string
	starts     int
	ends       int
	seenSQL    string
	endSawKeys []string
}

type recordingKey string

func (r *recordingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.starts++
	r.seenSQL = data.SQL
	return context.WithValue(ctx, recordingKey(r.name), r.name)
}

func (r *recordingTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	r.ends++
	r.endSawKeys = nil
	for _, key := range []string{"a", "b"} {
		if ctx.Value(recordingKey(key)) != nil {
			r.endSawKeys = append(r.endSawKeys, key)
		}
	}
}

func TestMultiQueryTracer_FansOutAndChainsContext(t *testing.T) {
	first := &recordingTracer{name: "a"}
	second := &recordingTracer{name: "b"}
	multi := NewMultiQueryTracer(first, second)

	ctx := multi.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	multi.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	assert.Equal(t, 1, first.starts)
	assert.Equal(t, 1, first.ends)
	assert.Equal(t, "SELECT 1", first.seenSQL)
	assert.Equal(t, 1, second.starts)
	assert.Equal(t, 1, second.ends)

	// Context values written by both Start hooks survive to both End hooks.
	assert.Equal(t, []string{"a", "b"}, first.endSawKeys)
	assert.Equal(t, []string{"a", "b"}, second.endSawKeys)
}

func TestMultiQueryTracer_WithMetricsTracer(t *testing.T) {
	metrics.Init("db-multi-tracer-test")

	multi := NewMultiQueryTracer(&recordingTracer{name: "a"}, MetricsQueryTracer{})

	ctx := multi.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "UPDATE mentors SET name = $1"})
	multi.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	require.NotNil(t, metrics.DBRequestTotal)
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.DBRequestTotal.WithLabelValues("update", "success")))
}
