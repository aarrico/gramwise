package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aarrico/gramwise/internal/db"
	"github.com/aarrico/gramwise/internal/ingest"
)

var datasetNames = map[string]string{
	"foundation": "foundation_food",
	"sr_legacy":  "sr_legacy_food",
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("ingest failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	source := flag.String("source", "", "https URL, .zip path, or directory of FDC CSVs")
	datasetsFlag := flag.String("datasets", "foundation,sr_legacy", "comma-separated: foundation, sr_legacy")
	maxMalformedPct := flag.Float64("max-malformed-pct", ingest.DefaultMaxMalformedPct,
		"reject the run if more than this percent of input rows fail to parse")
	flag.Parse()

	if *source == "" {
		return errors.New("--source is required")
	}
	if *maxMalformedPct < 0 || *maxMalformedPct > 100 {
		return fmt.Errorf("--max-malformed-pct must be in [0, 100], got %v", *maxMalformedPct)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	datasets := make([]string, 0, len(datasetNames))
	for short := range strings.SplitSeq(*datasetsFlag, ",") {
		dt, ok := datasetNames[strings.TrimSpace(short)]
		if !ok {
			return fmt.Errorf("unknown dataset %q (want: foundation, sr_legacy)", short)
		}
		datasets = append(datasets, dt)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(ctx, dsn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	src, err := ingest.NewSource(ctx, *source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	logger.Info("ingest starting", "source", src.Name(), "datasets", datasets)
	sum, err := ingest.Run(ctx, pool, src, datasets, *maxMalformedPct)
	if err != nil {
		return err
	}
	logger.Info("ingest finished",
		"rows_staged", sum.RowsStaged,
		"rows_upserted", sum.RowsUpserted,
		"skipped", sum.Skipped,
		"malformed", sum.Malformed,
		"duration_ms", sum.Duration.Milliseconds(),
	)
	return nil
}
