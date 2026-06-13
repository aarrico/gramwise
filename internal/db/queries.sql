-- name: InsertIngestRun :exec
INSERT INTO ingest_runs (
    started_at, finished_at, source, datasets, rows_staged, rows_upserted, duration_ms
) VALUES ($1, $2, $3, $4, $5, $6, $7);