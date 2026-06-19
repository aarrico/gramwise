package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aarrico/gramwise/internal/db"
)

// The WHERE guard compares data columns only (never updated_at), so
// re-running identical data updates zero rows — that row count is the
// idempotency contract the golden test asserts.
const upsertSQL = `
INSERT INTO foods (fdc_id, description, dataset_source, protein_g, carbs_g, fat_g, kcal)
SELECT fdc_id, description, dataset_source, protein_g, carbs_g, fat_g, kcal
FROM staging_foods
ON CONFLICT (fdc_id) DO UPDATE SET
    description    = excluded.description,
    dataset_source = excluded.dataset_source,
    protein_g      = excluded.protein_g,
    carbs_g        = excluded.carbs_g,
    fat_g          = excluded.fat_g,
    kcal           = excluded.kcal,
    updated_at     = now()
WHERE (foods.description, foods.dataset_source, foods.protein_g,
       foods.carbs_g, foods.fat_g, foods.kcal)
   IS DISTINCT FROM
      (excluded.description, excluded.dataset_source, excluded.protein_g,
       excluded.carbs_g, excluded.fat_g, excluded.kcal)`

// DefaultMaxMalformedPct is the default ceiling for the share of input rows
// that may fail to parse before a run is rejected. Clean official data sits at
// ~0%, so this is a blunt tripwire for a wrong/corrupt/format-changed file, not
// a precise quality SLA.
const DefaultMaxMalformedPct = 1.0

// checkMalformedRate rejects a parse whose malformed rows exceed maxPct of all
// rows read. Empty input never trips it.
func checkMalformedRate(res *ParseResult, maxPct float64) error {
	if res.RowsRead == 0 {
		return nil
	}
	rate := float64(res.Malformed) / float64(res.RowsRead) * 100
	if rate > maxPct {
		return fmt.Errorf("malformed row rate %.2f%% (%d of %d rows) exceeds limit %.2f%%",
			rate, res.Malformed, res.RowsRead, maxPct)
	}
	return nil
}

type RunSummary struct {
	Source       string
	Datasets     []string
	RowsStaged   int
	RowsUpserted int
	Skipped      int
	Malformed    int
	Duration     time.Duration
}

// Run executes one full ingest: parse, stage via COPY, set-based upsert,
// audit row. The malformed-rate guard runs after parsing and before any DB
// work, so a too-dirty source is rejected without touching foods. All DB work
// happens in a single transaction — a failure anywhere leaves foods untouched.
func Run(ctx context.Context, pool *pgxpool.Pool, src Source, datasets []string, maxMalformedPct float64) (*RunSummary, error) {
	started := time.Now().UTC()

	set := make(map[string]bool, len(datasets))
	for _, d := range datasets {
		set[d] = true
	}
	parsed, err := Parse(ctx, src, set)
	if err != nil {
		return nil, err
	}
	if err := checkMalformedRate(parsed, maxMalformedPct); err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit

	if _, err := tx.Exec(ctx, "TRUNCATE staging_foods"); err != nil {
		return nil, fmt.Errorf("truncate staging: %w", err)
	}

	staged, err := tx.CopyFrom(ctx,
		pgx.Identifier{"staging_foods"},
		[]string{"fdc_id", "description", "dataset_source", "protein_g", "carbs_g", "fat_g", "kcal"},
		pgx.CopyFromSlice(len(parsed.Foods), func(i int) ([]any, error) {
			f := parsed.Foods[i]
			return []any{f.FDCID, f.Description, f.DatasetSource, f.ProteinG, f.CarbsG, f.FatG, f.Kcal}, nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("copy to staging: %w", err)
	}

	tag, err := tx.Exec(ctx, upsertSQL)
	if err != nil {
		return nil, fmt.Errorf("upsert: %w", err)
	}

	finished := time.Now().UTC()
	sum := &RunSummary{
		Source:       src.Name(),
		Datasets:     datasets,
		RowsStaged:   int(staged),
		RowsUpserted: int(tag.RowsAffected()),
		Skipped:      parsed.Skipped,
		Malformed:    parsed.Malformed,
		Duration:     finished.Sub(started),
	}

	err = db.New(tx).InsertIngestRun(ctx, db.InsertIngestRunParams{
		StartedAt:     started,
		FinishedAt:    finished,
		Source:        sum.Source,
		Datasets:      sum.Datasets,
		RowsStaged:    int32(sum.RowsStaged),
		RowsUpserted:  int32(sum.RowsUpserted),
		RowsSkipped:   int32(sum.Skipped),
		RowsMalformed: int32(sum.Malformed),
		DurationMs:    sum.Duration.Milliseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("record ingest run: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return sum, nil
}
