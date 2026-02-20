package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/service"
)

// NewRouter wires all routes.
func NewRouter(pantry *service.PantryService, ingest *service.IngestService, dict *clients.DictionaryClient) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealth)

	r.Get("/pantry", handleListPantry(pantry))
	r.Post("/pantry/items", handleAddItem(pantry, dict))
	r.Delete("/pantry/items/{id}", handleDeleteItem(pantry))
	r.Post("/pantry/ingest", handleIngest(ingest))
	r.Get("/pantry/ingest/{job_id}", handleGetJob(ingest))
	r.Post("/pantry/ingest/{job_id}/confirm", handleConfirmJob(pantry, ingest))
	r.Delete("/pantry/reset", handleReset(pantry))

	return r
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok")) //nolint:errcheck
}

// --- GET /pantry ---

func handleListPantry(pantry *service.PantryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := pantry.ListItems(r.Context())
		if err != nil {
			jsonError(w, "failed to list pantry items", http.StatusInternalServerError)
			return
		}
		jsonOK(w, map[string]any{"items": items})
	}
}

// --- POST /pantry/items ---

type addItemRequest struct {
	Name         string  `json:"name"`          // raw text → resolved via Dictionary
	IngredientID string  `json:"ingredient_id"` // direct canonical ID (takes precedence)
	Quantity     float64 `json:"quantity"`
	Unit         string  `json:"unit"`
	ExpiresAt    *string `json:"expires_at"` // ISO 8601 or null
}

func handleAddItem(pantry *service.PantryService, dict *clients.DictionaryClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req addItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Quantity <= 0 {
			jsonError(w, "quantity must be positive", http.StatusBadRequest)
			return
		}
		if req.Unit == "" {
			jsonError(w, "unit is required", http.StatusBadRequest)
			return
		}

		var ingredientID uuid.UUID
		if req.IngredientID != "" {
			id, err := uuid.Parse(req.IngredientID)
			if err != nil {
				jsonError(w, "invalid ingredient_id", http.StatusBadRequest)
				return
			}
			ingredientID = id
		} else if req.Name != "" {
			result, err := dict.Resolve(r.Context(), req.Name)
			if err != nil {
				jsonError(w, "failed to resolve ingredient: "+err.Error(), http.StatusBadGateway)
				return
			}
			ingredientID = result.Ingredient.ID
		} else {
			jsonError(w, "name or ingredient_id is required", http.StatusBadRequest)
			return
		}

		var expiresAt sql.NullTime
		if req.ExpiresAt != nil {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				jsonError(w, "expires_at must be RFC3339", http.StatusBadRequest)
				return
			}
			expiresAt = sql.NullTime{Time: t, Valid: true}
		}

		item, err := pantry.UpsertItem(r.Context(), ingredientID, req.Quantity, req.Unit, expiresAt)
		if err != nil {
			jsonError(w, "failed to save pantry item", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(item) //nolint:errcheck
	}
}

// --- DELETE /pantry/items/:id ---

func handleDeleteItem(pantry *service.PantryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			jsonError(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := pantry.DeleteItem(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "item not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to delete item", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- DELETE /pantry/reset?confirm=true ---

func handleReset(pantry *service.PantryService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("confirm") != "true" {
			jsonError(w, "pass ?confirm=true to clear all pantry items", http.StatusBadRequest)
			return
		}
		if err := pantry.Reset(r.Context()); err != nil {
			jsonError(w, "failed to reset pantry", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
