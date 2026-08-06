package grpcutil

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
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
	s.Health.SetServingStatus("", st)
}

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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
	case sig := <-sigCh:
		slog.Info("shutdown signal", "signal", sig.String())
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
		resp, err := handler(ctx, req)
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "grpc",
			"method", info.FullMethod,
			"duration", time.Since(start).String(),
			"err", err,
		)
		return resp, err
	}
}

func RecoveryUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic recovered", "method", info.FullMethod, "panic", r)
				err = fmt.Errorf("internal panic")
			}
		}()
		return handler(ctx, req)
	}
}

func Dial(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, addr, //nolint:staticcheck // DialContext still common; NewClient ok too
		grpc.WithInsecure(), // demo only
		grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
}
