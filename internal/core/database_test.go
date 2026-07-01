package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/duynhlab/cart-service/config"
)

// TestConnect_ParseError verifies Connect returns an error when the DSN cannot
// be parsed by pgxpool.ParseConfig (an invalid sslmode value is rejected).
func TestConnect_ParseError(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Host:    "localhost",
			Port:    "5432",
			Name:    "cart",
			User:    "cart",
			SSLMode: "bogus",
		},
	}

	pool, err := Connect(context.Background(), cfg)
	require.Error(t, err)
	require.Nil(t, pool)
}

// TestConnect_PingError verifies Connect returns an error when the target host
// is unreachable, so the Ping after pool creation fails.
func TestConnect_PingError(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Host:           "127.0.0.1",
			Port:           "1",
			Name:           "cart",
			User:           "cart",
			SSLMode:        "disable",
			MaxConnections: 25,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := Connect(ctx, cfg)
	require.Error(t, err)
	require.Nil(t, pool)
}
