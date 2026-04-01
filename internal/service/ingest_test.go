package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/mocks"
)

func TestProcessJob_Success(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	rawInput := "2 cups flour, 1 lb chicken breast"
	garlicID := uuid.New()
	chickenID := uuid.New()

	// LLM returns extracted items
	mockLLM.EXPECT().Extract(mock.Anything, rawInput).Return(&ExtractionResponse{
		Items: []ExtractedItem{
			{RawText: "2 cups flour", Name: "flour", Quantity: 2.0, Unit: "cup", Confidence: 0.95},
			{RawText: "1 lb chicken breast", Name: "chicken breast", Quantity: 1.0, Unit: "lb", Confidence: 0.9},
		},
	}, nil)

	// Dictionary resolves each item
	mockDict.EXPECT().Resolve(mock.Anything, "flour").Return(clients.ResolveResult{
		Ingredient: struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}{ID: garlicID, Name: "flour"},
		Confidence: 0.95,
		Created:    false,
	}, nil)
	mockDict.EXPECT().Resolve(mock.Anything, "chicken breast").Return(clients.ResolveResult{
		Ingredient: struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}{ID: chickenID, Name: "chicken breast"},
		Confidence: 0.92,
		Created:    false,
	}, nil)

	// Staged items created
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{UUID: garlicID, Valid: true},
		RawText:      "2 cups flour",
		Quantity:     2.0,
		Unit:         "cup",
		Confidence:   0.95,
		NeedsReview:  false,
	}).Return(db.StagedItem{}, nil)

	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{UUID: chickenID, Valid: true},
		RawText:      "1 lb chicken breast",
		Quantity:     1.0,
		Unit:         "lb",
		Confidence:   0.9,
		NeedsReview:  false,
	}).Return(db.StagedItem{}, nil)

	// Job status updated to staged
	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	}).Return(db.IngestionJob{}, nil)

	err := svc.processJob(context.Background(), jobID, rawInput)
	require.NoError(t, err)
}

func TestProcessJob_LLMFailure(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	rawInput := "some groceries"

	mockLLM.EXPECT().Extract(mock.Anything, rawInput).Return(nil, errors.New("openai timeout"))

	err := svc.processJob(context.Background(), jobID, rawInput)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm extraction")
}

func TestProcessJob_DictionaryFailureSetsNeedsReview(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	rawInput := "1 bunch cilantro"

	mockLLM.EXPECT().Extract(mock.Anything, rawInput).Return(&ExtractionResponse{
		Items: []ExtractedItem{
			{RawText: "1 bunch cilantro", Name: "cilantro", Quantity: 1.0, Unit: "bunch", Confidence: 0.9},
		},
	}, nil)

	// Dictionary fails
	mockDict.EXPECT().
		Resolve(mock.Anything, "cilantro").
		Return(clients.ResolveResult{}, errors.New("connection refused"))

	// Staged item created with needs_review=true and empty ingredient_id
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{},
		RawText:      "1 bunch cilantro",
		Quantity:     1.0,
		Unit:         "bunch",
		Confidence:   0.9,
		NeedsReview:  true,
	}).Return(db.StagedItem{}, nil)

	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	}).Return(db.IngestionJob{}, nil)

	err := svc.processJob(context.Background(), jobID, rawInput)
	require.NoError(t, err)
}

func TestConfirmJob_SuccessWithOverrides(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)

	ingestSvc := NewIngestService(mockQ, mockDict, mockLLM)
	pantrySvc := NewPantryService(mockQ)

	jobID := uuid.New()
	stagedItemID := uuid.New()
	ingredientID := uuid.New()
	overrideIngredientID := uuid.New()
	now := time.Now()

	// GetIngestionJob returns staged job
	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "text_blob",
		RawInput:  "test",
		Status:    "staged",
		CreatedAt: now,
	}, nil)

	// ListStagedItemsByJob returns one item
	mockQ.EXPECT().ListStagedItemsByJob(mock.Anything, jobID).Return([]db.StagedItem{
		{
			ID:           stagedItemID,
			JobID:        jobID,
			IngredientID: uuid.NullUUID{UUID: ingredientID, Valid: true},
			RawText:      "2 cups flour",
			Quantity:     2.0,
			Unit:         "cup",
			Confidence:   0.9,
			NeedsReview:  false,
		},
	}, nil)

	// Override changes ingredient_id and quantity
	overrideQty := 3.0
	overrides := []OverrideItem{
		{
			StagedItemID: stagedItemID,
			IngredientID: &overrideIngredientID,
			Quantity:     &overrideQty,
		},
	}

	// UpsertPantryItem called with overridden values
	mockQ.EXPECT().UpsertPantryItem(mock.Anything, db.UpsertPantryItemParams{
		IngredientID: overrideIngredientID,
		Quantity:     3.0,
		Unit:         "cup",
		ExpiresAt:    sql.NullTime{},
	}).Return(db.PantryItem{}, nil)

	// Job status updated to confirmed
	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "confirmed",
	}).Return(db.IngestionJob{}, nil)

	err := ingestSvc.ConfirmJob(context.Background(), jobID, pantrySvc, overrides)
	require.NoError(t, err)
}

func TestConfirmJob_SkipsItemsWithoutIngredientID(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)

	ingestSvc := NewIngestService(mockQ, mockDict, mockLLM)
	pantrySvc := NewPantryService(mockQ)

	jobID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "text_blob",
		RawInput:  "test",
		Status:    "staged",
		CreatedAt: now,
	}, nil)

	// Staged item with no ingredient_id
	mockQ.EXPECT().ListStagedItemsByJob(mock.Anything, jobID).Return([]db.StagedItem{
		{
			ID:           uuid.New(),
			JobID:        jobID,
			IngredientID: uuid.NullUUID{Valid: false},
			RawText:      "mystery item",
			Quantity:     1.0,
			Unit:         "piece",
			Confidence:   0.5,
			NeedsReview:  true,
		},
	}, nil)

	// UpsertPantryItem should NOT be called — item is skipped

	// Job status updated to confirmed
	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "confirmed",
	}).Return(db.IngestionJob{}, nil)

	err := ingestSvc.ConfirmJob(context.Background(), jobID, pantrySvc, nil)
	require.NoError(t, err)
}

func TestStageItems_Success(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	ingredientID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "queue_ingest",
		RawInput:  "2 cups flour, mystery item",
		Status:    "pending",
		CreatedAt: now,
	}, nil)

	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{UUID: ingredientID, Valid: true},
		RawText:      "2 cups flour",
		Quantity:     2.0,
		Unit:         "cup",
		Confidence:   0.95,
		NeedsReview:  false,
	}).Return(db.StagedItem{}, nil)

	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{},
		RawText:      "mystery item",
		Quantity:     1.0,
		Unit:         "piece",
		Confidence:   0.5,
		NeedsReview:  true,
	}).Return(db.StagedItem{}, nil)

	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	}).Return(db.IngestionJob{}, nil)

	result, err := svc.StageItems(context.Background(), jobID, []StagedItemInput{
		{RawText: "2 cups flour", IngredientID: &ingredientID, Quantity: 2.0, Unit: "cup", Confidence: 0.95},
		{RawText: "mystery item", IngredientID: nil, Quantity: 1.0, Unit: "piece", Confidence: 0.5},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.StagedCount)
	assert.Equal(t, 1, result.NeedsReviewCount)
}

func TestStageItems_JobNotFound(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{}, sql.ErrNoRows)

	_, err := svc.StageItems(context.Background(), jobID, []StagedItemInput{
		{RawText: "flour", Quantity: 1.0, Unit: "cup", Confidence: 0.9},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestStageItems_WrongStatus(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "queue_ingest",
		RawInput:  "test",
		Status:    "staged",
		CreatedAt: now,
	}, nil)

	_, err := svc.StageItems(context.Background(), jobID, []StagedItemInput{
		{RawText: "flour", Quantity: 1.0, Unit: "cup", Confidence: 0.9},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be pending or processing to stage")
}

func TestStageItems_MixedResolvedUnresolved(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)
	svc := NewIngestService(mockQ, mockDict, mockLLM)

	jobID := uuid.New()
	resolvedID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "queue_ingest",
		RawInput:  "test",
		Status:    "processing",
		CreatedAt: now,
	}, nil)

	// Resolved item with high confidence
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{UUID: resolvedID, Valid: true},
		RawText:      "2 lbs chicken",
		Quantity:     2.0,
		Unit:         "lb",
		Confidence:   0.95,
		NeedsReview:  false,
	}).Return(db.StagedItem{}, nil)

	// Resolved item with low confidence → needs review
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{UUID: resolvedID, Valid: true},
		RawText:      "a thing of cream",
		Quantity:     1.0,
		Unit:         "carton",
		Confidence:   0.5,
		NeedsReview:  true,
	}).Return(db.StagedItem{}, nil)

	// Unresolved item → needs review regardless of confidence
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{},
		RawText:      "unknown stuff",
		Quantity:     1.0,
		Unit:         "piece",
		Confidence:   0.8,
		NeedsReview:  true,
	}).Return(db.StagedItem{}, nil)

	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	}).Return(db.IngestionJob{}, nil)

	result, err := svc.StageItems(context.Background(), jobID, []StagedItemInput{
		{RawText: "2 lbs chicken", IngredientID: &resolvedID, Quantity: 2.0, Unit: "lb", Confidence: 0.95},
		{RawText: "a thing of cream", IngredientID: &resolvedID, Quantity: 1.0, Unit: "carton", Confidence: 0.5},
		{RawText: "unknown stuff", IngredientID: nil, Quantity: 1.0, Unit: "piece", Confidence: 0.8},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, result.StagedCount)
	assert.Equal(t, 2, result.NeedsReviewCount)
}

func TestConfirmJob_WrongStatusError(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	mockDict := NewMockDictionaryResolver(t)
	mockLLM := NewMockLLMExtractor(t)

	ingestSvc := NewIngestService(mockQ, mockDict, mockLLM)
	pantrySvc := NewPantryService(mockQ)

	jobID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "text_blob",
		RawInput:  "test",
		Status:    "pending",
		CreatedAt: now,
	}, nil)

	err := ingestSvc.ConfirmJob(context.Background(), jobID, pantrySvc, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be staged to confirm")
}
