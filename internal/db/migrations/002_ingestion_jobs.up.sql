CREATE TABLE IF NOT EXISTS ingestion_jobs (
  id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  type       TEXT        NOT NULL,
  raw_input  TEXT        NOT NULL,
  status     TEXT        NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS staged_items (
  id            UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id        UUID    NOT NULL REFERENCES ingestion_jobs(id) ON DELETE CASCADE,
  ingredient_id UUID,
  raw_text      TEXT    NOT NULL,
  quantity      FLOAT8  NOT NULL,
  unit          TEXT    NOT NULL,
  confidence    FLOAT8  NOT NULL,
  needs_review  BOOLEAN NOT NULL DEFAULT false
);
