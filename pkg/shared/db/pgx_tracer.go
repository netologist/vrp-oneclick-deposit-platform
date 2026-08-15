package db

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

type tracerCtxKey struct{}

type queryTraceState struct {
	startTime time.Time
	sql       string
}

// LoggingQueryTracer implements pgx.QueryTracer, logging query execution time and alerting on slow queries.
type LoggingQueryTracer struct {
	SlowThreshold time.Duration
}

func (t *LoggingQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, tracerCtxKey{}, &queryTraceState{
		startTime: time.Now(),
		sql:       data.SQL,
	})
}

func (t *LoggingQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(tracerCtxKey{}).(*queryTraceState)
	if !ok || state == nil {
		return
	}
	duration := time.Since(state.startTime)
	threshold := t.SlowThreshold
	if threshold <= 0 {
		threshold = 100 * time.Millisecond
	}

	if data.Err != nil {
		slog.ErrorContext(ctx, "db_query_error",
			"sql", state.sql,
			"duration_ms", duration.Milliseconds(),
			"err", data.Err,
		)
		return
	}

	if duration > threshold {
		slog.WarnContext(ctx, "db_slow_query",
			"sql", state.sql,
			"duration_ms", duration.Milliseconds(),
			"threshold_ms", threshold.Milliseconds(),
		)
		return
	}

	slog.DebugContext(ctx, "db_query",
		"sql", state.sql,
		"duration_ms", duration.Milliseconds(),
	)
}
