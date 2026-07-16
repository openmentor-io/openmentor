package db

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// metricsCtxKey is the private context key used to carry per-query state
// from TraceQueryStart to TraceQueryEnd.
type metricsCtxKey struct{}

// queryMetricsData is the per-query state stored in the context.
type queryMetricsData struct {
	start     time.Time
	operation string
}

// MetricsQueryTracer is a pgx.QueryTracer that records the Prometheus
// db_client_operation_duration_seconds / db_client_operation_total metrics
// for every query executed through the pool.
//
// Labels are bounded: operation is the first SQL keyword lowercased
// (select/insert/update/delete/begin/commit, anything else -> "other"),
// status is success|error.
type MetricsQueryTracer struct{}

// TraceQueryStart implements pgx.QueryTracer.
func (MetricsQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, metricsCtxKey{}, &queryMetricsData{
		start:     time.Now(),
		operation: queryOperation(data.SQL),
	})
}

// TraceQueryEnd implements pgx.QueryTracer.
func (MetricsQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	qd, ok := ctx.Value(metricsCtxKey{}).(*queryMetricsData)
	if !ok {
		return
	}
	// Metrics may not be initialized (e.g. in tests or tooling that builds a
	// pool without calling metrics.Init); skip recording in that case.
	if metrics.DBRequestDuration == nil || metrics.DBRequestTotal == nil {
		return
	}

	status := "success"
	if data.Err != nil {
		status = "error"
	}

	metrics.DBRequestDuration.WithLabelValues(qd.operation, status).Observe(time.Since(qd.start).Seconds())
	metrics.DBRequestTotal.WithLabelValues(qd.operation, status).Inc()
}

// queryOperation extracts a bounded-cardinality operation label from a SQL
// statement: the first keyword lowercased when it is one of the well-known
// verbs, "other" for everything else (including empty statements).
func queryOperation(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "other"
	}
	switch op := strings.ToLower(fields[0]); op {
	case "select", "insert", "update", "delete", "begin", "commit":
		return op
	default:
		return "other"
	}
}

// MultiQueryTracer fans a single pgx tracer slot out to multiple tracers
// (pgx v5 allows exactly one Tracer per connection config). It implements
// pgx.QueryTracer directly and passes Batch/Connect/CopyFrom tracing through
// to any member tracer that implements the corresponding optional interface.
//
// Start hooks are chained: the context returned by one tracer's Start is
// passed to the next, so each tracer's End hook can read the values its own
// Start stored in the context.
type MultiQueryTracer struct {
	Tracers []pgx.QueryTracer
}

// NewMultiQueryTracer creates a MultiQueryTracer fanning out to the given tracers.
func NewMultiQueryTracer(tracers ...pgx.QueryTracer) *MultiQueryTracer {
	return &MultiQueryTracer{Tracers: tracers}
}

// TraceQueryStart implements pgx.QueryTracer.
func (m *MultiQueryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, t := range m.Tracers {
		ctx = t.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

// TraceQueryEnd implements pgx.QueryTracer.
func (m *MultiQueryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for _, t := range m.Tracers {
		t.TraceQueryEnd(ctx, conn, data)
	}
}

// TraceBatchStart implements pgx.BatchTracer for members that support it.
func (m *MultiQueryTracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	for _, t := range m.Tracers {
		if bt, ok := t.(pgx.BatchTracer); ok {
			ctx = bt.TraceBatchStart(ctx, conn, data)
		}
	}
	return ctx
}

// TraceBatchQuery implements pgx.BatchTracer for members that support it.
func (m *MultiQueryTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	for _, t := range m.Tracers {
		if bt, ok := t.(pgx.BatchTracer); ok {
			bt.TraceBatchQuery(ctx, conn, data)
		}
	}
}

// TraceBatchEnd implements pgx.BatchTracer for members that support it.
func (m *MultiQueryTracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	for _, t := range m.Tracers {
		if bt, ok := t.(pgx.BatchTracer); ok {
			bt.TraceBatchEnd(ctx, conn, data)
		}
	}
}

// TraceConnectStart implements pgx.ConnectTracer for members that support it.
func (m *MultiQueryTracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	for _, t := range m.Tracers {
		if ct, ok := t.(pgx.ConnectTracer); ok {
			ctx = ct.TraceConnectStart(ctx, data)
		}
	}
	return ctx
}

// TraceConnectEnd implements pgx.ConnectTracer for members that support it.
func (m *MultiQueryTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	for _, t := range m.Tracers {
		if ct, ok := t.(pgx.ConnectTracer); ok {
			ct.TraceConnectEnd(ctx, data)
		}
	}
}

// TraceCopyFromStart implements pgx.CopyFromTracer for members that support it.
func (m *MultiQueryTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	for _, t := range m.Tracers {
		if ct, ok := t.(pgx.CopyFromTracer); ok {
			ctx = ct.TraceCopyFromStart(ctx, conn, data)
		}
	}
	return ctx
}

// TraceCopyFromEnd implements pgx.CopyFromTracer for members that support it.
func (m *MultiQueryTracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	for _, t := range m.Tracers {
		if ct, ok := t.(pgx.CopyFromTracer); ok {
			ct.TraceCopyFromEnd(ctx, conn, data)
		}
	}
}
