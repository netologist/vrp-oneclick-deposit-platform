package logutil

import (
	"context"
	"log/slog"
)

type ctxKey string

const (
	// TraceIDKey is the context key for distributed trace ID.
	TraceIDKey ctxKey = "trace_id"
	// SpanIDKey is the context key for distributed span ID.
	SpanIDKey ctxKey = "span_id"
	// RequestIDKey is the context key for HTTP/gRPC request ID.
	RequestIDKey ctxKey = "request_id"
)

// WithTraceContext returns a context enriched with trace and span IDs.
func WithTraceContext(ctx context.Context, traceID, spanID string) context.Context {
	if traceID != "" {
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
	}
	if spanID != "" {
		ctx = context.WithValue(ctx, SpanIDKey, spanID)
	}
	return ctx
}

// WithRequestID returns a context enriched with a request ID.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID != "" {
		ctx = context.WithValue(ctx, RequestIDKey, requestID)
	}
	return ctx
}

// TraceHandler is an slog.Handler middleware that extracts trace_id, span_id,
// and request_id from context.Context and appends them as structured attributes.
type TraceHandler struct {
	slog.Handler
}

// NewTraceHandler wraps an existing slog.Handler with trace context extraction.
func NewTraceHandler(h slog.Handler) *TraceHandler {
	return &TraceHandler{Handler: h}
}

// Handle extracts trace attributes from ctx before passing the record to the wrapped handler.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		if reqID, ok := ctx.Value(RequestIDKey).(string); ok && reqID != "" {
			r.AddAttrs(slog.String("request_id", reqID))
		}
		if traceID, ok := ctx.Value(TraceIDKey).(string); ok && traceID != "" {
			r.AddAttrs(slog.String("trace_id", traceID))
		}
		if spanID, ok := ctx.Value(SpanIDKey).(string); ok && spanID != "" {
			r.AddAttrs(slog.String("span_id", spanID))
		}
	}
	return h.Handler.Handle(ctx, r)
}
