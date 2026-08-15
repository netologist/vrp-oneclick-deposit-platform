package grpcutil

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/logutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type Server struct {
	GRPC   *grpc.Server
	Health *health.Server
	addr   string
}

func NewServer(addr string, opts ...grpc.ServerOption) *Server {
	base := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(LoggingUnary(), RecoveryUnary()),
	}
	base = append(base, opts...)
	gs := grpc.NewServer(base...)
	hs := health.NewServer()
	healthpb.RegisterHealthServer(gs, hs)
	reflection.Register(gs)
	return &Server{GRPC: gs, Health: hs, addr: addr}
}

func (s *Server) SetServing(service string, serving bool) {
	st := healthpb.HealthCheckResponse_SERVING
	if !serving {
		st = healthpb.HealthCheckResponse_NOT_SERVING
	}
	s.Health.SetServingStatus(service, st)
}

// Serve starts the gRPC listener and blocks until ctx is cancelled or an error occurs.
// When ctx is cancelled, it performs graceful drain and shutdown.
func (s *Server) Serve(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("grpc listening", "addr", s.addr)
		errCh <- s.GRPC.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutdown triggered, draining grpc server", "addr", s.addr)
	case err := <-errCh:
		return err
	}

	s.Health.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	stopped := make(chan struct{})
	go func() {
		s.GRPC.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(15 * time.Second):
		s.GRPC.Stop()
	}
	return nil
}

func LoggingUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// Extract incoming metadata (x-request-id, traceparent) and enrich context
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if reqIDs := md.Get("x-request-id"); len(reqIDs) > 0 && reqIDs[0] != "" {
				ctx = logutil.WithRequestID(ctx, reqIDs[0])
			}
			if tp := md.Get("traceparent"); len(tp) > 0 && tp[0] != "" {
				// W3C traceparent format: 00-<trace_id>-<span_id>-<flags>
				parts := strings.Split(tp[0], "-")
				if len(parts) >= 3 {
					ctx = logutil.WithTraceContext(ctx, parts[1], parts[2])
				}
			}
		}

		resp, err := handler(ctx, req)
		level := slog.LevelInfo
		attrs := []any{
			"method", info.FullMethod,
			"duration", time.Since(start).String(),
		}
		if err != nil {
			level = slog.LevelWarn
			attrs = append(attrs, "err", err)
		}
		slog.Log(ctx, level, "grpc", attrs...)
		return resp, err
	}
}

func RecoveryUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic recovered", "method", info.FullMethod, "panic", r)
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// Dial creates a client connection with modern grpc.NewClient and keepalive parameters.
func Dial(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.MaxCallRecvMsgSize(4 * 1024 * 1024),
		),
	}
	return grpc.NewClient(addr, opts...)
}
