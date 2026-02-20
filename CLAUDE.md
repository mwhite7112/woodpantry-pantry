# woodpantry-pantry — Pantry Service

## Role in Architecture

Owns current pantry/inventory state. Tracks what ingredients are in the kitchen right now, at what quantity, and when they expire. This service is the live inventory that the Matching Service reads to score recipe availability.

All pantry items reference canonical Ingredient Dictionary IDs — raw ingredient strings are resolved through the Dictionary before any item is stored.

All ingest flows (text blob, SMS) follow the **staged commit pattern**: raw input in → LLM extraction → staged items for review → user confirms → committed to pantry state.

After any stock change, the Pantry Service publishes a `pantry.updated` event (Phase 2+) so downstream consumers like the Matching Service can invalidate caches.

## Technology

- Language: Go
- HTTP: chi
- Database: PostgreSQL (`pantry_db`) via sqlc
- RabbitMQ (Phase 2+): publishes `pantry.updated`, subscribes to `pantry.ingest.requested`
- LLM: OpenAI API (`gpt-5-mini`) for text extraction (Phase 1 direct call). In Phase 2+, LLM extraction moves to the Ingestion Pipeline.

## Service Dependencies

- **Calls**: Ingredient Dictionary (`/ingredients/resolve` per item on ingest)
- **Called by**: Matching Service (current pantry state), Shopping List Service (current pantry state), Ingestion Pipeline (commit staged items, Phase 2+)
- **Publishes** (Phase 2+): `pantry.updated`
- **Subscribes to** (Phase 2+): `pantry.ingest.requested`

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/pantry` | Current pantry state — all items with quantities |
| POST | `/pantry/items` | Manually add or update a single pantry item |
| DELETE | `/pantry/items/:id` | Remove a pantry item |
| POST | `/pantry/ingest` | Submit text blob for LLM extraction and staging |
| GET | `/pantry/ingest/:job_id` | Ingest job status + staged items for review |
| POST | `/pantry/ingest/:job_id/confirm` | Commit staged items; can edit before confirming |
| DELETE | `/pantry/reset` | Clear all pantry items (before a full re-stock) |

## Key Patterns

### Staged Ingest
`POST /pantry/ingest` does not immediately update the pantry. It creates an `IngestionJob` with status `pending`, triggers LLM extraction, and updates the job to `staged` with parsed items. Each staged item includes: `raw_text`, resolved `ingredient_id` (or null if unmatched), `quantity`, `unit`, `confidence`, `needs_review`. The user reviews and calls the confirm endpoint.

### Write-Through to Dictionary
On every ingest confirm, each staged item's `raw_text` is resolved via `POST /ingredients/resolve`. The returned canonical `ingredient_id` is stored in `pantry_items`. Raw strings are never stored as the ingredient reference.

### Quantity Tracking
Full quantity tracking with unit per item. The `unit` field stores the unit string as provided (e.g. "g", "lb", "bunch"). Unit normalization for comparison happens in the Shopping List Service using unit conversion data from the Dictionary.

### Idempotent Item Updates
`POST /pantry/items` for an ingredient that already exists in the pantry should update the quantity rather than creating a duplicate entry. Use upsert semantics on `ingredient_id`.

## Data Models

```
pantry_items
  id              UUID  PK
  ingredient_id   UUID  -- canonical ID from Dictionary
  quantity        FLOAT8
  unit            TEXT
  expires_at      TIMESTAMPTZ  NULLABLE
  added_at        TIMESTAMPTZ
  updated_at      TIMESTAMPTZ

ingestion_jobs
  id              UUID  PK
  type            TEXT  -- text_blob|sms|receipt_image
  raw_input       TEXT  -- original text or image path
  status          TEXT  -- pending|processing|staged|confirmed|failed
  created_at      TIMESTAMPTZ

staged_items
  id              UUID  PK
  job_id          UUID  FK
  ingredient_id   UUID  NULLABLE  -- null if not matched to Dictionary
  raw_text        TEXT
  quantity        FLOAT8
  unit            TEXT
  confidence      FLOAT8  -- LLM confidence 0.0–1.0
  needs_review    BOOL
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `DB_URL` | required | PostgreSQL connection string for `pantry_db` |
| `DICTIONARY_URL` | required | Ingredient Dictionary service base URL |
| `OPENAI_API_KEY` | required | OpenAI API key for text extraction |
| `EXTRACT_MODEL` | `gpt-5-mini` | OpenAI model for extraction |
| `RABBITMQ_URL` | optional | Enables event publishing and queue subscription (Phase 2+) |
| `LOG_LEVEL` | `info` | Log level |

## Directory Layout

```
woodpantry-pantry/
├── cmd/pantry/main.go
├── internal/
│   ├── api/
│   │   ├── handlers.go
│   │   └── ingest.go
│   ├── db/
│   │   ├── migrations/
│   │   ├── queries/
│   │   └── sqlc.yaml
│   ├── service/
│   │   ├── pantry.go          ← item CRUD, upsert logic
│   │   └── ingest.go          ← LLM extraction + staged commit (Phase 1 direct, Phase 2+ via queue)
│   └── events/
│       └── publisher.go       ← publish pantry.updated (Phase 2+)
├── kubernetes/
├── Dockerfile
├── go.mod
└── go.sum
```

## What to Avoid

- Do not store raw ingredient strings as the primary ingredient reference — always resolve to a Dictionary ID.
- Do not allow `DELETE /pantry/reset` without an explicit confirmation parameter — accidental resets are destructive.
- Do not add RabbitMQ in Phase 1 — LLM extraction happens synchronously until Phase 2.
- Do not duplicate ingredient metadata (name, category, aliases) in this DB — only store the `ingredient_id` FK.
- Do not fail the HTTP response if RabbitMQ publish fails (Phase 2+) — log the error and continue.
