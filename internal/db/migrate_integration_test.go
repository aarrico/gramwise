//go:build integration

package db_test

import (
	"context"
	"math"
	"testing"

	"github.com/aarrico/gramwise/internal/dbtest"
)

func TestMigration0004Schema(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)

	var dataType string
	if err := pool.QueryRow(ctx,
		`SELECT data_type FROM information_schema.columns
		 WHERE table_name = 'foods' AND column_name = 'description_tsv'`).Scan(&dataType); err != nil {
		t.Fatalf("description_tsv column missing: %v", err)
	}
	if dataType != "tsvector" {
		t.Errorf("description_tsv type = %q, want tsvector", dataType)
	}

	var one int
	if err := pool.QueryRow(ctx,
		`SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm'`).Scan(&one); err != nil {
		t.Fatalf("pg_trgm extension missing: %v", err)
	}

	for _, idx := range []string{"foods_description_tsv_gin", "foods_description_trgm_gin"} {
		var name string
		if err := pool.QueryRow(ctx,
			`SELECT indexname FROM pg_indexes WHERE indexname = $1`, idx).Scan(&name); err != nil {
			t.Errorf("index %s missing: %v", idx, err)
		}
	}

	// The WHERE uses a pg_trgm operator to force the extension's module (and its
	// custom GUC) to load on this exact connection before reading the setting.
	var threshold float64
	if err := pool.QueryRow(ctx,
		`SELECT current_setting('pg_trgm.word_similarity_threshold')::float8
		 WHERE word_similarity('a', 'a') >= 0`).Scan(&threshold); err != nil {
		t.Fatalf("read word_similarity_threshold: %v", err)
	}
	if math.Abs(threshold-0.3) > 1e-9 {
		t.Errorf("word_similarity_threshold = %v, want 0.3", threshold)
	}
}
