-- name: CreateIngestionJob :one
INSERT INTO ingestion_jobs (type, raw_input)
VALUES ($1, $2)
RETURNING id, type, raw_input, status, created_at;

-- name: GetIngestionJob :one
SELECT id, type, raw_input, status, created_at
FROM ingestion_jobs
WHERE id = $1;

-- name: UpdateIngestionJobStatus :one
UPDATE ingestion_jobs
SET status = $2
WHERE id = $1
RETURNING id, type, raw_input, status, created_at;

-- name: CreateStagedItem :one
INSERT INTO staged_items (job_id, ingredient_id, raw_text, quantity, unit, confidence, needs_review)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, job_id, ingredient_id, raw_text, quantity, unit, confidence, needs_review;

-- name: ListStagedItemsByJob :many
SELECT id, job_id, ingredient_id, raw_text, quantity, unit, confidence, needs_review
FROM staged_items
WHERE job_id = $1
ORDER BY raw_text;

-- name: GetStagedItem :one
SELECT id, job_id, ingredient_id, raw_text, quantity, unit, confidence, needs_review
FROM staged_items
WHERE id = $1;

-- name: UpdateStagedItem :one
UPDATE staged_items
SET ingredient_id = $2,
    quantity      = $3,
    unit          = $4
WHERE id = $1
RETURNING id, job_id, ingredient_id, raw_text, quantity, unit, confidence, needs_review;
