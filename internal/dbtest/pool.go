//go:build integration

package dbtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/aarrico/gramwise/internal/db"
)

// NewPool starts a disposable Postgres, runs all migrations, and returns a
// ready connection pool. The container and pool are cleaned up automatically
// when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("gramwise_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pgC)
	if err != nil {
		t.Fatal(err)
	}

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// SeedFoods inserts a small fixed fixture: three chicken cuts and one
// non-matching broccoli row. Later search assertions depend on these exact
// rows and IDs.
func SeedFoods(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO foods (fdc_id, description, dataset_source, protein_g, carbs_g, fat_g, kcal) VALUES
		(100001, 'Chicken, breast, raw',    'sr_legacy_food',  31,  0, 3.6, 165),
		(100002, 'Chicken, thigh, raw',     'sr_legacy_food',  26,  0, 11,  209),
		(100003, 'Chicken, drumstick, raw', 'sr_legacy_food',  24,  0, 8,   172),
		(100004, 'Broccoli, raw',           'foundation_food', 2.8, 7, 0.4, 34)`)
	if err != nil {
		t.Fatalf("seed foods: %v", err)
	}
}
