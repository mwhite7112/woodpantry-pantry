# woodpantry-pantry

Pantry Service for WoodPantry. Owns current inventory state — what ingredients are in the kitchen, at what quantity, and when they expire. All items reference canonical Ingredient Dictionary IDs.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | Health check |
| GET | `/pantry` | Current pantry state — all items with quantities |
| POST | `/pantry/items` | Add or update a single pantry item |
| DELETE | `/pantry/items/:id` | Remove a pantry item |
| POST | `/pantry/ingest` | Submit a grocery list text for LLM extraction and staging |
| GET | `/pantry/ingest/:job_id` | Get ingest job status and staged items for review |
| POST | `/pantry/ingest/:job_id/confirm` | Commit staged items to pantry |
| DELETE | `/pantry/reset` | Clear all pantry items |

### GET /pantry

```json
{
  "items": [
    { "id": "uuid", "ingredient_id": "uuid", "name": "garlic", "quantity": 3, "unit": "clove", "expires_at": null }
  ]
}
```

### POST /pantry/ingest

Accepts a free-text grocery list. Triggers LLM extraction and returns a job ID.

```json
// Request
{
  "type": "text_blob",
  "content": "2 lbs chicken breast, 1 head garlic, a thing of heavy cream, 3 bell peppers"
}

// Response
{ "job_id": "uuid", "status": "pending" }
```

### GET /pantry/ingest/:job_id

```json
{
  "status": "staged",
  "items": [
    { "raw_text": "2 lbs chicken breast", "ingredient_id": "uuid", "quantity": 2, "unit": "lb", "confidence": 0.97, "needs_review": false },
    { "raw_text": "a thing of heavy cream", "ingredient_id": null, "quantity": 1, "unit": "carton", "confidence": 0.61, "needs_review": true }
  ]
}
```

### POST /pantry/ingest/:job_id/confirm

Commits staged items. Optionally include edited items in the body to override staged values before committing.

```json
// Optional body — override specific staged items before commit
{
  "overrides": [
    { "staged_item_id": "uuid", "quantity": 2, "unit": "cup", "ingredient_id": "uuid" }
  ]
}
```

## Ingest Flow

```
POST /pantry/ingest
  → LLM extracts ingredient list with quantities
  → Each item resolved via POST /ingredients/resolve
  → Staged as IngestionJob
GET /pantry/ingest/:job_id    ← review staged items
POST /pantry/ingest/:job_id/confirm
  → Staged items committed to pantry_items
  → pantry.updated event published (Phase 2+)
```

## Events (Phase 2+)

| Event | Direction | Description |
|-------|-----------|-------------|
| `pantry.updated` | Publishes | After any stock change — item add, update, delete, ingest confirm, reset |
| `pantry.ingest.requested` | Subscribes | Triggers ingest pipeline processing (Phase 2+) |

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `DB_URL` | required | PostgreSQL `pantry_db` connection string |
| `DICTIONARY_URL` | required | Ingredient Dictionary service base URL |
| `OPENAI_API_KEY` | required | OpenAI API key for text extraction |
| `EXTRACT_MODEL` | `gpt-5-mini` | OpenAI model for extraction |
| `RABBITMQ_URL` | optional | Enables event publishing (Phase 2+) |
| `LOG_LEVEL` | `info` | Log level |

## Development

```bash
go run ./cmd/pantry/main.go
sqlc generate -f internal/db/sqlc.yaml
```
