-- +goose Up
ALTER TABLE ingest_runs
    ADD COLUMN rows_skipped   int NOT NULL DEFAULT 0,
    ADD COLUMN rows_malformed int NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE ingest_runs
    DROP COLUMN rows_malformed,
    DROP COLUMN rows_skipped;
