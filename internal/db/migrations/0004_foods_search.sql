-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE foods
    ADD COLUMN description_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', description)) STORED;

CREATE INDEX foods_description_tsv_gin  ON foods USING gin (description_tsv);
CREATE INDEX foods_description_trgm_gin ON foods USING gin (description gin_trgm_ops);

-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER DATABASE %I SET pg_trgm.word_similarity_threshold = 0.3', current_database());
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER DATABASE %I RESET pg_trgm.word_similarity_threshold', current_database());
END $$;
-- +goose StatementEnd
DROP INDEX foods_description_trgm_gin;
DROP INDEX foods_description_tsv_gin;
ALTER TABLE foods DROP COLUMN description_tsv;
DROP EXTENSION IF EXISTS pg_trgm;