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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/mocks"
	"github.com/mwhite7112/woodpantry-pantry/internal/service"
)

func setupRouter(t *testing.T) (*mocks.MockQuerier, http.Handler) {
	t.Helper()

	mockQ := mocks.NewMockQuerier(t)
	pantrySvc := service.NewPantryService(mockQ)
	ingestSvc := service.NewIngestService(mockQ, &stubResolver{}, &stubExtractor{})

	dictServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ingredients/"):
			id := strings.TrimPrefix(r.URL.Path, "/ingredients/")
			json.NewEncoder(w).Encode(map[string]string{
				"ID":   id,
				"Name": "ingredient-" + id,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/ingredients/resolve":
			json.NewEncoder(w).Encode(clients.ResolveResult{
				Ingredient: struct {
					ID   uuid.UUID `json:"id"`
					Name string    `json:"name"`
				}{ID: uuid.New(), Name: "resolved-ingredient"},
				Confidence: 1,
				Created:    false,
			})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
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

	var raw map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &raw)
	require.NoError(t, err)
	itemsValue, ok := raw["items"].([]any)
	require.True(t, ok)
	require.Len(t, itemsValue, 1)

	item, ok := itemsValue[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, items[0].ID.String(), item["id"])
	assert.Equal(t, items[0].IngredientID.String(), item["ingredient_id"])
	assert.Equal(t, "ingredient-"+items[0].IngredientID.String(), item["name"])
	assert.Equal(t, items[0].Quantity, item["quantity"])
	assert.Equal(t, items[0].Unit, item["unit"])
	assert.Contains(t, item, "expires_at")
	assert.Nil(t, item["expires_at"])
	assert.NotEmpty(t, item["added_at"])
	assert.NotEmpty(t, item["updated_at"])
	assert.NotContains(t, item, "IngredientID")
	assert.NotContains(t, item, "Name")
	assert.NotContains(t, item, "Quantity")
	assert.NotContains(t, item, "Unit")
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

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, expected.ID.String(), got["id"])
	assert.Equal(t, ingredientID.String(), got["ingredient_id"])
	assert.Equal(t, "ingredient-"+ingredientID.String(), got["name"])
	assert.Equal(t, 1.5, got["quantity"])
	assert.Equal(t, "lb", got["unit"])
	assert.NotContains(t, got, "IngredientID")
	assert.NotContains(t, got, "Name")
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
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/ingredients/resolve":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(clients.ResolveResult{
				Ingredient: struct {
					ID   uuid.UUID `json:"id"`
					Name string    `json:"name"`
				}{ID: ingredientID, Name: "garlic"},
				Confidence: 0.95,
				Created:    false,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/ingredients/"+ingredientID.String():
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"ID":   ingredientID.String(),
				"Name": "garlic",
			})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
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

	var got struct {
		ID           string  `json:"id"`
		IngredientID string  `json:"ingredient_id"`
		Name         string  `json:"name"`
		Quantity     float64 `json:"quantity"`
		Unit         string  `json:"unit"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, expected.ID.String(), got.ID)
	assert.Equal(t, ingredientID.String(), got.IngredientID)
	assert.Equal(t, "garlic", got.Name)
	assert.Equal(t, 3.0, got.Quantity)
	assert.Equal(t, "clove", got.Unit)
}

func TestPostPantryItems_MissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing name and ingredient_id", `{"quantity":1,"unit":"cup"}`, "name or ingredient_id is required"},
		{
			"missing quantity",
			`{"ingredient_id":"` + uuid.New().String() + `","quantity":0,"unit":"cup"}`,
			"quantity must be positive",
		},
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
