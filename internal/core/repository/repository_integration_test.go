//go:build integration

// Integration tests for PostgresCartRepository against a real PostgreSQL
// started via testcontainers-go. The service migrations are applied as init
// scripts, so the actual SQL is exercised (not a mock). Run with:
//
//	go test -tags=integration ./internal/core/repository/...
//
// Requires a reachable Docker daemon. Excluded from the default `go test ./...`
// unit run by the `integration` build tag.
package repository

import (
	"context"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/duynhlab/cart-service/internal/core/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// newTestRepo starts a throwaway Postgres with the service migrations applied
// and returns a repository wired to it. Everything is torn down via t.Cleanup.
func newTestRepo(t *testing.T) *PostgresCartRepository {
	t.Helper()
	ctx := context.Background()

	files, err := filepath.Glob("../../../db/migrations/sql/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migration files found: %v", err)
	}
	sort.Strings(files)
	for i := range files {
		if abs, aerr := filepath.Abs(files[i]); aerr == nil {
			files[i] = abs
		}
	}

	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("cart"),
		postgres.WithUsername("cart"),
		postgres.WithPassword("secret"),
		postgres.WithInitScripts(files...),
		// Postgres logs "ready to accept connections" twice: once after the
		// init scripts run, then again on the real start. Waiting for the 2nd
		// occurrence avoids connecting during the initdb restart (conn reset).
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewPostgresCartRepository(pool)
}

func TestCartRepository_Integration(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	const userID = "990001" // high id unlikely to collide with seed data

	// empty cart
	if c, err := repo.FindByUserID(ctx, userID); err != nil || len(c.Items) != 0 {
		t.Fatalf("empty FindByUserID = (%+v, %v), want empty cart", c, err)
	}
	if n, err := repo.GetItemCount(ctx, userID); err != nil || n != 0 {
		t.Fatalf("empty GetItemCount = (%d, %v), want 0", n, err)
	}

	// add item
	item := &domain.CartItem{ProductID: "5001", ProductName: "Widget", ProductPrice: 9.99, Quantity: 2}
	if err := repo.AddItem(ctx, userID, item); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if item.ID == "" {
		t.Error("AddItem did not populate item.ID")
	}

	// upsert: same product accumulates quantity
	dup := &domain.CartItem{ProductID: "5001", ProductName: "Widget", ProductPrice: 9.99, Quantity: 3}
	if err := repo.AddItem(ctx, userID, dup); err != nil {
		t.Fatalf("AddItem (upsert): %v", err)
	}
	if n, err := repo.GetItemCount(ctx, userID); err != nil || n != 5 {
		t.Fatalf("GetItemCount after upsert = (%d, %v), want 5", n, err)
	}
	if cart, err := repo.FindByUserID(ctx, userID); err != nil || len(cart.Items) != 1 {
		t.Fatalf("FindByUserID = (%+v, %v), want exactly 1 line item", cart, err)
	}

	// update quantity
	if err := repo.UpdateItem(ctx, userID, item.ID, 4); err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if err := repo.UpdateItem(ctx, userID, "999999", 1); err != domain.ErrNotFound {
		t.Errorf("UpdateItem(missing) = %v, want ErrNotFound", err)
	}

	// remove
	if err := repo.RemoveItem(ctx, userID, item.ID); err != nil {
		t.Fatalf("RemoveItem: %v", err)
	}
	if err := repo.RemoveItem(ctx, userID, item.ID); err != domain.ErrNotFound {
		t.Errorf("RemoveItem(missing) = %v, want ErrNotFound", err)
	}

	// clear
	if err := repo.AddItem(ctx, userID, &domain.CartItem{ProductID: "6001", ProductName: "X", ProductPrice: 1, Quantity: 1}); err != nil {
		t.Fatalf("AddItem before Clear: %v", err)
	}
	if err := repo.Clear(ctx, userID); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if n, _ := repo.GetItemCount(ctx, userID); n != 0 {
		t.Errorf("GetItemCount after Clear = %d, want 0", n)
	}
}
