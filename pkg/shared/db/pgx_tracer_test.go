package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/db"
	"github.com/stretchr/testify/assert"
)

func TestLoggingQueryTracer(t *testing.T) {
	t.Parallel()

	tracer := &db.LoggingQueryTracer{SlowThreshold: 50 * time.Millisecond}

	t.Run("records and ends normal query without panic", func(t *testing.T) {
		ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
			SQL: "SELECT 1",
		})
		assert.NotNil(t, ctx)

		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
			Err: nil,
		})
	})

	t.Run("handles error query without panic", func(t *testing.T) {
		ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
			SQL: "SELECT * FROM non_existent_table",
		})
		assert.NotNil(t, ctx)

		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
			Err: errors.New("table does not exist"),
		})
	})
}
