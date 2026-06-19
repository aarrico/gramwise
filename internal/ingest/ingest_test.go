//go:build integration

package ingest_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/aarrico/gramwise/internal/db"
	"github.com/aarrico/gramwise/internal/ingest"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestIngestGoldenAndIdempotency(t *testing.T) {
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
	defer pool.Close()

	datasets := []string{"foundation_food", "sr_legacy_food"}
	runOnce := func() *ingest.RunSummary {
		t.Helper()
		src, err := ingest.NewSource(ctx, "testdata/fdc_small")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = src.Close() }()
		sum, err := ingest.Run(ctx, pool, src, datasets, ingest.DefaultMaxMalformedPct)
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}

	sum := runOnce()
	if sum.RowsStaged != 4 || sum.RowsUpserted != 4 || sum.Skipped != 1 || sum.Malformed != 0 {
		t.Fatalf("first run: staged=%d upserted=%d skipped=%d malformed=%d, want 4/4/1/0",
			sum.RowsStaged, sum.RowsUpserted, sum.Skipped, sum.Malformed)
	}

	got := dumpFoods(ctx, t, pool)
	const goldenPath = "testdata/golden/foods.json"
	if *update {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var want [][]string
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foods mismatch\n got: %v\nwant: %v", got, want)
	}

	// Idempotency: identical input must change nothing.
	sum2 := runOnce()
	if sum2.RowsUpserted != 0 {
		t.Errorf("second run upserted %d rows, want 0", sum2.RowsUpserted)
	}
	if got2 := dumpFoods(ctx, t, pool); !reflect.DeepEqual(got2, got) {
		t.Errorf("foods table changed on re-run")
	}

	var runs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM ingest_runs").Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Errorf("ingest_runs count = %d, want 2", runs)
	}

	var skipped, malformed int
	if err := pool.QueryRow(ctx,
		"SELECT rows_skipped, rows_malformed FROM ingest_runs ORDER BY id LIMIT 1").
		Scan(&skipped, &malformed); err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || malformed != 0 {
		t.Errorf("ingest_runs row counts = skipped %d, malformed %d; want 1, 0", skipped, malformed)
	}
}

func dumpFoods(ctx context.Context, t *testing.T, pool *pgxpool.Pool) [][]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT fdc_id::text, description, dataset_source,
		       protein_g::text, carbs_g::text, fat_g::text, kcal::text
		FROM foods ORDER BY fdc_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out [][]string
	for rows.Next() {
		row := make([]string, 7)
		ptrs := make([]any, 7)
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A source whose malformed rate exceeds the guard is rejected before any DB
// work: Run errors and foods, staging_foods, and ingest_runs stay empty.
func TestIngestRejectsTooManyMalformed(t *testing.T) {
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
	defer pool.Close()

	// Every food row has a non-integer fdc_id: 100% malformed, far above the guard.
	dirty := mapSource{
		"food.csv": `"fdc_id","data_type","description"
"notanumber","foundation_food","Bad row 1"
"alsobad","foundation_food","Bad row 2"
`,
		"food_nutrient.csv": `"id","fdc_id","nutrient_id","amount"
`,
	}

	_, err = ingest.Run(ctx, pool, dirty, []string{"foundation_food", "sr_legacy_food"}, ingest.DefaultMaxMalformedPct)
	if err == nil {
		t.Fatal("Run succeeded on a fully-malformed source, want guard error")
	}

	for _, table := range []string{"foods", "staging_foods", "ingest_runs"} {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s has %d rows after a rejected run, want 0", table, n)
		}
	}
}
