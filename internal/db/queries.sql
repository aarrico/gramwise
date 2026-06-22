-- name: InsertIngestRun :exec
INSERT INTO
    ingest_runs (
        started_at,
        finished_at,
        source,
        datasets,
        rows_staged,
        rows_upserted,
        rows_skipped,
        rows_malformed,
        duration_ms
    )
VALUES
    ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: SearchFoods :many
SELECT
    fdc_id,
    description,
    dataset_source,
    protein_g,
    carbs_g,
    fat_g,
    kcal,
    count(*) OVER() as total
FROM
    foods
WHERE
    description_tsv @@ websearch_to_tsquery('english', @query)
    OR @query <% description
ORDER BY
    (
        description_tsv @@ websearch_to_tsquery('english', @query)
    ) DESC,
    ts_rank(
        description_tsv,
        websearch_to_tsquery('english', @query)
    ) DESC,
    word_similarity(@query, description) DESC,
    fdc_id ASC
LIMIT
    @result_limit OFFSET @result_offset;