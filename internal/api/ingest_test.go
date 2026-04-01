package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/mocks"
	"github.com/mwhite7112/woodpantry-pantry/internal/service"
)

// stubExtractor is a no-op LLM extractor for handler tests where we don't
// need async processing to complete.
type stubExtractor struct{}

func (s *stubExtractor) Extract(_ context.Context, _ string) (*service.ExtractionResponse, error) {
	return &service.ExtractionResponse{Items: []service.ExtractedItem{}}, nil
}

// stubResolver is a no-op dictionary resolver for handler tests.
type stubResolver struct{}

func (s *stubResolver) Resolve(_ context.Context, _ string) (clients.ResolveResult, error) {
	return clients.ResolveResult{}, nil
}

func setupIngestRouter(t *testing.T) (*mocks.MockQuerier, http.Handler) {
	t.Helper()

	mockQ := mocks.NewMockQuerier(t)

	pantrySvc := service.NewPantryService(mockQ)
	ingestSvc := service.NewIngestService(mockQ, &stubResolver{}, &stubExtractor{})

	dictServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(dictServer.Close)
	dictClient := clients.NewDictionaryClient(dictServer.URL, dictServer.Client())

	router := NewRouter(pantrySvc, ingestSvc, dictClient)
	return mockQ, router
}

func TestPostIngest_Success(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().CreateIngestionJob(mock.Anything, db.CreateIngestionJobParams{
		Type:     "text_blob",
		RawInput: "2 cups flour",
	}).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "text_blob",
		RawInput:  "2 cups flour",
		Status:    "pending",
		CreatedAt: now,
	}, nil)

	// ProcessJobAsync runs in a goroutine — set up optional expectations
	mockQ.On("UpdateIngestionJobStatus", mock.Anything, mock.Anything).Return(db.IngestionJob{}, nil).Maybe()

	body := `{"content":"2 cups flour"}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)

	var result map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, jobID.String(), result["job_id"])
	assert.Equal(t, "pending", result["status"])
}

func TestPostIngest_MissingContent(t *testing.T) {
	t.Parallel()

	_, router := setupIngestRouter(t)

	body := `{"type":"text_blob"}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errBody map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Contains(t, errBody["error"], "content is required")
}

func TestGetIngestJob_Success(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	now := time.Now()
	ingredientID := uuid.New()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "text_blob",
		RawInput:  "test",
		Status:    "staged",
		CreatedAt: now,
	}, nil)

	stagedItems := []db.StagedItem{
		{
			ID:           uuid.New(),
			JobID:        jobID,
			IngredientID: uuid.NullUUID{UUID: ingredientID, Valid: true},
			RawText:      "2 cups flour",
			Quantity:     2.0,
			Unit:         "cup",
			Confidence:   0.95,
			NeedsReview:  false,
		},
	}
	mockQ.EXPECT().ListStagedItemsByJob(mock.Anything, jobID).Return(stagedItems, nil)

	req := httptest.NewRequest(http.MethodGet, "/pantry/ingest/"+jobID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, jobID.String(), result["job_id"])
	assert.Equal(t, "staged", result["status"])
	items, ok := result["items"].([]any)
	require.True(t, ok)
	assert.Len(t, items, 1)
}

func TestGetIngestJob_NotFound(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{}, sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/pantry/ingest/"+jobID.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostStageJob_Success(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	ingredientID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "queue_ingest",
		RawInput:  "2 cups flour",
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

	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	}).Return(db.IngestionJob{}, nil)

	body := `{"items":[{"raw_text":"2 cups flour","ingredient_id":"` + ingredientID.String() + `","quantity":2.0,"unit":"cup","confidence":0.95}]}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest/"+jobID.String()+"/stage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["staged_count"])
	assert.Equal(t, float64(0), result["needs_review_count"])
}

func TestPostStageJob_InvalidJobID(t *testing.T) {
	t.Parallel()

	_, router := setupIngestRouter(t)

	body := `{"items":[{"raw_text":"flour","quantity":1,"unit":"cup","confidence":0.9}]}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest/not-a-uuid/stage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errBody map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Contains(t, errBody["error"], "invalid job_id")
}

func TestPostStageJob_JobNotFound(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{}, sql.ErrNoRows)

	body := `{"items":[{"raw_text":"flour","quantity":1,"unit":"cup","confidence":0.9}]}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest/"+jobID.String()+"/stage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostStageJob_EmptyItems(t *testing.T) {
	t.Parallel()

	_, router := setupIngestRouter(t)

	jobID := uuid.New()
	body := `{"items":[]}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest/"+jobID.String()+"/stage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var errBody map[string]string
	err := json.Unmarshal(rec.Body.Bytes(), &errBody)
	require.NoError(t, err)
	assert.Contains(t, errBody["error"], "items")
}

func TestPostStageJob_MixedResolvedUnresolved(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	ingredientID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "queue_ingest",
		RawInput:  "test",
		Status:    "pending",
		CreatedAt: now,
	}, nil)

	// Resolved item
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{UUID: ingredientID, Valid: true},
		RawText:      "2 cups flour",
		Quantity:     2.0,
		Unit:         "cup",
		Confidence:   0.95,
		NeedsReview:  false,
	}).Return(db.StagedItem{}, nil)

	// Unresolved item
	mockQ.EXPECT().CreateStagedItem(mock.Anything, db.CreateStagedItemParams{
		JobID:        jobID,
		IngredientID: uuid.NullUUID{},
		RawText:      "mystery goo",
		Quantity:     1.0,
		Unit:         "piece",
		Confidence:   0.4,
		NeedsReview:  true,
	}).Return(db.StagedItem{}, nil)

	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "staged",
	}).Return(db.IngestionJob{}, nil)

	body := `{"items":[` +
		`{"raw_text":"2 cups flour","ingredient_id":"` + ingredientID.String() + `","quantity":2.0,"unit":"cup","confidence":0.95},` +
		`{"raw_text":"mystery goo","ingredient_id":null,"quantity":1.0,"unit":"piece","confidence":0.4}` +
		`]}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest/"+jobID.String()+"/stage", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var result map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, float64(2), result["staged_count"])
	assert.Equal(t, float64(1), result["needs_review_count"])
}

func TestPostConfirmJob(t *testing.T) {
	t.Parallel()

	mockQ, router := setupIngestRouter(t)

	jobID := uuid.New()
	stagedItemID := uuid.New()
	ingredientID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().GetIngestionJob(mock.Anything, jobID).Return(db.IngestionJob{
		ID:        jobID,
		Type:      "text_blob",
		RawInput:  "test",
		Status:    "staged",
		CreatedAt: now,
	}, nil)

	mockQ.EXPECT().ListStagedItemsByJob(mock.Anything, jobID).Return([]db.StagedItem{
		{
			ID:           stagedItemID,
			JobID:        jobID,
			IngredientID: uuid.NullUUID{UUID: ingredientID, Valid: true},
			RawText:      "2 cups flour",
			Quantity:     2.0,
			Unit:         "cup",
			Confidence:   0.95,
			NeedsReview:  false,
		},
	}, nil)

	mockQ.EXPECT().UpsertPantryItem(mock.Anything, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     2.0,
		Unit:         "cup",
		ExpiresAt:    sql.NullTime{},
	}).Return(db.PantryItem{}, nil)

	mockQ.EXPECT().UpdateIngestionJobStatus(mock.Anything, db.UpdateIngestionJobStatusParams{
		ID:     jobID,
		Status: "confirmed",
	}).Return(db.IngestionJob{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/pantry/ingest/"+jobID.String()+"/confirm", nil)
	req.ContentLength = 0
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}
