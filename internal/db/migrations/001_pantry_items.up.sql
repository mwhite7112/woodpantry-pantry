CREATE TABLE IF NOT EXISTS pantry_items (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  ingredient_id UUID        NOT NULL,
  quantity      FLOAT8      NOT NULL,
  unit          TEXT        NOT NULL,
  expires_at    TIMESTAMPTZ,
  added_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (ingredient_id)
);
