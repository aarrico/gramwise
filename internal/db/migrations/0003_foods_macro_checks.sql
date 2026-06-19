-- +goose Up
ALTER TABLE foods
    ADD CONSTRAINT foods_protein_g_nonneg CHECK (protein_g >= 0),
    ADD CONSTRAINT foods_carbs_g_nonneg   CHECK (carbs_g >= 0),
    ADD CONSTRAINT foods_fat_g_nonneg     CHECK (fat_g >= 0),
    ADD CONSTRAINT foods_kcal_nonneg      CHECK (kcal >= 0);

-- +goose Down
ALTER TABLE foods
    DROP CONSTRAINT foods_kcal_nonneg,
    DROP CONSTRAINT foods_fat_g_nonneg,
    DROP CONSTRAINT foods_carbs_g_nonneg,
    DROP CONSTRAINT foods_protein_g_nonneg;
