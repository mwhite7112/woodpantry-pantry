package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
)

// IngestService handles the staged ingest flow: LLM extraction → staging → confirm.
type IngestService struct {
	q          *db.Queries
	dictionary *clients.DictionaryClient
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewIngestService(q *db.Queries, dictionary *clients.DictionaryClient, apiKey, model string) *IngestService {
	return &IngestService{
		q:          q,
		dictionary: dictionary,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// CreateJob persists a new IngestionJob with status "pending".
func (s *IngestService) CreateJob(ctx context.Context, jobType, rawInput string) (db.IngestionJob, error) {
	return s.q.CreateIngestionJob(ctx, db.CreateIngestionJobParams{
		Type:     jobType,
		RawInput: rawInput,
	})
}

// ProcessJobAsync kicks off LLM extraction and ingredient resolution in the
// background. The job status is updated to "staged" on success or "failed" on
// error. Phase 2+ will replace this with a RabbitMQ consumer.
func (s *IngestService) ProcessJobAsync(jobID uuid.UUID, rawInput string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if err := s.processJob(ctx, jobID, rawInput); err != nil {
			slog.Error("ingest job failed", "job_id", jobID, "error", err)
			_, _ = s.q.UpdateIngestionJobStatus(ctx, db.UpdateIngestionJobStatusParams{
				ID:     jobID,
				Status: "failed",
			})
		}
	}()
}

func (s *IngestService) processJob(ctx context.Context, jobID uuid.UUID, rawInput string) error {
	slog.Info("LLM extraction starting", "job_id", jobID, "model", s.model, "input_len", len(rawInput))

	extracted, err := s.extractWithLLM(ctx, rawInput)
	if err != nil {
		return fmt.Errorf("llm extraction: %w", err)
	}

	slog.Info("LLM extraction complete", "job_id", jobID, "items", len(extracted.Items))

	for _, item := range extracted.Items {
		var ingredientID db.NullUUID
		needsReview := item.Confidence < 0.7

		result, resolveErr := s.dictionary.Resolve(ctx, item.Name)
		if resolveErr != nil {
			slog.Warn("dictionary resolve failed", "job_id", jobID, "name", item.Name, "error", resolveErr)
			needsReview = true
		} else {
			ingredientID = db.NullUUID{UUID: result.Ingredient.ID, Valid: true}
		}

		if _, err := s.q.CreateStagedItem(ctx, db.CreateStagedItemParams{
			JobID:        jobID,
			IngredientID: ingredientID,
			RawText:      item.RawText,
			Quantity:     item.Quantity,
			Unit:         item.Unit,
			Confidence:   item.Confidence,
			NeedsReview:  needsReview,
		}); err != nil {
			return fmt.Errorf("create staged item for %q: %w", item.RawText, err)
		}
	}

	_, err = s.q.UpdateIngestionJobStatus(ctx, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	})
	return err
}

// GetJob returns a single IngestionJob by ID.
func (s *IngestService) GetJob(ctx context.Context, id uuid.UUID) (db.IngestionJob, error) {
	return s.q.GetIngestionJob(ctx, id)
}

// ListStagedItems returns the staged items for a job.
func (s *IngestService) ListStagedItems(ctx context.Context, jobID uuid.UUID) ([]db.StagedItem, error) {
	items, err := s.q.ListStagedItemsByJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []db.StagedItem{}, nil
	}
	return items, nil
}

// OverrideItem allows the caller to edit a staged item before confirming.
type OverrideItem struct {
	StagedItemID uuid.UUID  `json:"staged_item_id"`
	IngredientID *uuid.UUID `json:"ingredient_id,omitempty"`
	Quantity     *float64   `json:"quantity,omitempty"`
	Unit         *string    `json:"unit,omitempty"`
}

// ConfirmJob commits staged items to the pantry. Optional overrides let the
// caller adjust quantity, unit, or ingredient_id before commit. Items without a
// resolved ingredient_id are skipped with a warning.
func (s *IngestService) ConfirmJob(ctx context.Context, jobID uuid.UUID, pantry *PantryService, overrides []OverrideItem) error {
	job, err := s.q.GetIngestionJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != "staged" {
		return fmt.Errorf("job %s has status %q, must be staged to confirm", jobID, job.Status)
	}

	staged, err := s.q.ListStagedItemsByJob(ctx, jobID)
	if err != nil {
		return err
	}

	overrideMap := make(map[uuid.UUID]OverrideItem, len(overrides))
	for _, o := range overrides {
		overrideMap[o.StagedItemID] = o
	}

	for _, item := range staged {
		ingredientID := item.IngredientID
		quantity := item.Quantity
		unit := item.Unit

		if o, ok := overrideMap[item.ID]; ok {
			if o.IngredientID != nil {
				ingredientID = db.NullUUID{UUID: *o.IngredientID, Valid: true}
			}
			if o.Quantity != nil {
				quantity = *o.Quantity
			}
			if o.Unit != nil {
				unit = *o.Unit
			}
		}

		if !ingredientID.Valid {
			slog.Warn("skipping staged item: no ingredient_id resolved", "item_id", item.ID, "raw_text", item.RawText)
			continue
		}

		if _, err := pantry.UpsertItem(ctx, ingredientID.UUID, quantity, unit, sql.NullTime{}); err != nil {
			return fmt.Errorf("upsert pantry item for staged item %s: %w", item.ID, err)
		}
	}

	_, err = s.q.UpdateIngestionJobStatus(ctx, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "confirmed",
	})
	return err
}

// --- LLM extraction ---

type extractedItem struct {
	RawText    string  `json:"raw_text"`
	Name       string  `json:"name"`
	Quantity   float64 `json:"quantity"`
	Unit       string  `json:"unit"`
	Confidence float64 `json:"confidence"`
}

type extractionResponse struct {
	Items []extractedItem `json:"items"`
}

const systemPrompt = `You are a grocery list parser. Extract ingredients with quantities from the user's text.

Return a JSON object with an "items" array. Each item must have:
- "raw_text": the original text snippet for this item
- "name": the canonical ingredient name (lowercase, singular, normalized — e.g. "chicken breast" not "2 lbs chicken breasts")
- "quantity": numeric quantity as a float (default 1.0 if unclear)
- "unit": unit of measure (e.g. "lb", "g", "cup", "oz", "bunch", "head", "clove", "piece", "carton")
- "confidence": your confidence 0.0 to 1.0

For ambiguous or unclear items set confidence below 0.7.
For items where the unit is unclear, use "piece".`

func (s *IngestService) extractWithLLM(ctx context.Context, text string) (*extractionResponse, error) {
	payload := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
		"response_format": map[string]string{"type": "json_object"},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai status %d: %s", resp.StatusCode, string(raw))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("openai response decode: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	var extracted extractionResponse
	if err := json.Unmarshal([]byte(chatResp.Choices[0].Message.Content), &extracted); err != nil {
		return nil, fmt.Errorf("parse extraction json: %w", err)
	}
	return &extracted, nil
}
