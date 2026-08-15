package logutil_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/logutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	baseHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := logutil.NewTraceHandler(baseHandler)
	logger := slog.New(handler)

	t.Run("injects trace_id, span_id, and request_id from context", func(t *testing.T) {
		buf.Reset()
		ctx := logutil.WithTraceContext(context.Background(), "trace-abc-123", "span-xyz-789")
		ctx = logutil.WithRequestID(ctx, "req-555")

		logger.InfoContext(ctx, "test message", "merchant_id", "m-1")

		var logEntry map[string]any
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "test message", logEntry["msg"])
		assert.Equal(t, "trace-abc-123", logEntry["trace_id"])
		assert.Equal(t, "span-xyz-789", logEntry["span_id"])
		assert.Equal(t, "req-555", logEntry["request_id"])
		assert.Equal(t, "m-1", logEntry["merchant_id"])
	})

	t.Run("handles context without trace attributes cleanly", func(t *testing.T) {
		buf.Reset()
		logger.InfoContext(context.Background(), "plain message")

		var logEntry map[string]any
		err := json.Unmarshal(buf.Bytes(), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "plain message", logEntry["msg"])
		assert.Nil(t, logEntry["trace_id"])
		assert.Nil(t, logEntry["span_id"])
		assert.Nil(t, logEntry["request_id"])
	})
}
