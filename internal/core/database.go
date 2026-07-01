package database

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/duynhlab/cart-service/config"
)

// Connect establishes a database connection pool using pgx/v5.
//
// Why pgx instead of lib/pq?
//   - pgx uses client-side prepared statements, compatible with PgCat/PgBouncer
//     transaction mode.
//   - lib/pq uses server-side prepared statements which cause errors with
//     connection poolers:
//     "pq: bind message supplies 1 parameters, but prepared statement "" requires 2"
//   - pgxpool provides built-in connection pooling optimized for PostgreSQL.
//
// IMPORTANT: We use SimpleProtocol mode and disable statement caching to work
// correctly with transaction-mode connection poolers (PgCat/PgBouncer). Without
// this, you may see: "prepared statement stmtcache_* does not exist".
//
// The DSN is the single source of truth from config.DatabaseConfig, identical to
// the one the `migrate` subcommand uses; pool sizing is applied on the parsed
// config so the DSN stays the same across app and migrate.
func Connect(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.BuildDSN())
	if err != nil {
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	if cfg.Database.MaxConnections > 0 && cfg.Database.MaxConnections <= math.MaxInt32 {
		// Bounds-checked just above; a pool size never overflows int32.
		poolCfg.MaxConns = int32(cfg.Database.MaxConnections) // #nosec G115
	}

	// Configure for transaction-mode poolers (PgCat/PgBouncer):
	// - Use simple protocol to avoid server-side prepared statements
	// - Disable statement cache (prepared statements are connection-scoped)
	// - Disable description cache
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	poolCfg.ConnConfig.StatementCacheCapacity = 0
	poolCfg.ConnConfig.DescriptionCacheCapacity = 0

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection is working
	if err := pool.Ping(ctx); err != nil {
		pool.Close() // Clean up on failure
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}
