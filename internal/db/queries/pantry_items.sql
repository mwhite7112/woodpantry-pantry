-- name: ListPantryItems :many
SELECT id, ingredient_id, quantity, unit, expires_at, added_at, updated_at
FROM pantry_items
ORDER BY added_at;

-- name: GetPantryItem :one
SELECT id, ingredient_id, quantity, unit, expires_at, added_at, updated_at
FROM pantry_items
WHERE id = $1;

-- name: UpsertPantryItem :one
INSERT INTO pantry_items (ingredient_id, quantity, unit, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (ingredient_id) DO UPDATE
  SET quantity   = EXCLUDED.quantity,
      unit       = EXCLUDED.unit,
      expires_at = EXCLUDED.expires_at,
      updated_at = now()
RETURNING id, ingredient_id, quantity, unit, expires_at, added_at, updated_at;

-- name: DeletePantryItem :exec
DELETE FROM pantry_items WHERE id = $1;

-- name: DeleteAllPantryItems :exec
DELETE FROM pantry_items;
