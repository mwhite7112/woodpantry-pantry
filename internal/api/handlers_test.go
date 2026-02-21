package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/mocks"
	"github.com/mwhite7112/woodpantry-pantry/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupRouter(t *testing.T) (*mocks.MockQuerier, http.Handler) {
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

func TestGetPantry(t *testing.T) {
	t.Parallel()

	mockQ, router := setupRouter(t)

	now := time.Now()
	items := []db.PantryItem{
		{
			ID:           uuid.New(),
			IngredientID: uuid.New(),
			Quantity:     2.0,
			Unit:         "cup",
			ExpiresAt:    sql.NullTime{},
			AddedAt:      now,
			UpdatedAt:    now,
		},
	}
	mockQ.EXPECT().ListPantryItems(mock.Anything).Return(items, nil)

	req := httptest.NewRequest(http.MethodGet, "/pantry", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]json.RawMessage
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Contains(t, string(body["items"]), items[0].ID.String())
}

func TestPostPantryItems_WithIngredientID(t *testing.T) {
	t.Parallel()

	mockQ, router := setupRouter(t)

	ingredientID := uuid.New()
	now := time.Now()
	expected := db.PantryItem{
		ID:           uuid.New(),
		IngredientID: ingredientID,
		Quantity:     1.5,
		Unit:         "lb",
		ExpiresAt:    sql.NullTime{},
		AddedAt:      now,
		UpdatedAt:    now,
	}

	mockQ.EXPECT().UpsertPantryItem(mock.Anything, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     1.5,
		Unit:         "lb",
		ExpiresAt:    sql.NullTime{},
	}).Return(expected, nil)

	body := `{"ingredient_id":"` + ingredientID.String() + `","quantity":1.5,"unit":"lb"}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/items", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestPostPantryItems_WithName(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	pantrySvc := service.NewPantryService(mockQ)
	ingestSvc := service.NewIngestService(mockQ, &stubResolver{}, &stubExtractor{})

	ingredientID := uuid.New()
	now := time.Now()

	dictServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(clients.ResolveResult{
			Ingredient: struct {
				ID   uuid.UUID `json:"id"`
				Name string    `json:"name"`
			}{ID: ingredientID, Name: "garlic"},
			Confidence: 0.95,
			Created:    false,
		})
	}))
	defer dictServer.Close()

	dictClient := clients.NewDictionaryClient(dictServer.URL, dictServer.Client())
	router := NewRouter(pantrySvc, ingestSvc, dictClient)

	expected := db.PantryItem{
		ID:           uuid.New(),
		IngredientID: ingredientID,
		Quantity:     3.0,
		Unit:         "clove",
		ExpiresAt:    sql.NullTime{},
		AddedAt:      now,
		UpdatedAt:    now,
	}

	mockQ.EXPECT().UpsertPantryItem(mock.Anything, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     3.0,
		Unit:         "clove",
		ExpiresAt:    sql.NullTime{},
	}).Return(expected, nil)

	body := `{"name":"garlic","quantity":3,"unit":"clove"}`
	req := httptest.NewRequest(http.MethodPost, "/pantry/items", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestPostPantryItems_MissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing name and ingredient_id", `{"quantity":1,"unit":"cup"}`, "name or ingredient_id is required"},
		{"missing quantity", `{"ingredient_id":"` + uuid.New().String() + `","quantity":0,"unit":"cup"}`, "quantity must be positive"},
		{"missing unit", `{"ingredient_id":"` + uuid.New().String() + `","quantity":1,"unit":""}`, "unit is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, router := setupRouter(t)

			req := httptest.NewRequest(http.MethodPost, "/pantry/items", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			var errBody map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
			assert.Contains(t, errBody["error"], tc.want)
		})
	}
}

func TestDeletePantryItem(t *testing.T) {
	t.Parallel()

	mockQ, router := setupRouter(t)

	id := uuid.New()
	mockQ.EXPECT().DeletePantryItem(mock.Anything, id).Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/pantry/items/"+id.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeletePantryReset_WithConfirm(t *testing.T) {
	t.Parallel()

	mockQ, router := setupRouter(t)

	mockQ.EXPECT().DeleteAllPantryItems(mock.Anything).Return(nil)

	req := httptest.NewRequest(http.MethodDelete, "/pantry/reset?confirm=true", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestDeletePantryReset_WithoutConfirm(t *testing.T) {
	t.Parallel()

	_, router := setupRouter(t)

	req := httptest.NewRequest(http.MethodDelete, "/pantry/reset", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
