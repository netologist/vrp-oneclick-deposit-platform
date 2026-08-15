package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/config"
)

// NewPool creates a PostgreSQL connection pool with configurable limits and healthchecks.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}

	maxConns := int32(config.GetInt("DB_MAX_CONNS", 20))
	minConns := int32(config.GetInt("DB_MIN_CONNS", 2))
	idleTime := config.GetDuration("DB_MAX_IDLE_TIME", 30*time.Minute)
	lifeTime := config.GetDuration("DB_MAX_LIFETIME", time.Hour)

	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	if minConns >= 0 {
		cfg.MinConns = minConns
	}
	cfg.MaxConnLifetime = lifeTime
	cfg.MaxConnIdleTime = idleTime
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}
