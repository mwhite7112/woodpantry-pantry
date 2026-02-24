package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/mwhite7112/woodpantry-pantry/internal/service"
)

// --- POST /pantry/ingest ---

type ingestRequest struct {
	Type    string `json:"type"`    // text_blob
	Content string `json:"content"` // raw grocery list text
}

func handleIngest(ingest *service.IngestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ingestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Content == "" {
			jsonError(w, "content is required", http.StatusBadRequest)
			return
		}
		jobType := req.Type
		if jobType == "" {
			jobType = "text_blob"
		}

		job, err := ingest.CreateJob(r.Context(), jobType, req.Content)
		if err != nil {
			jsonError(w, "failed to create ingest job", http.StatusInternalServerError, err)
			return
		}

		ingest.ProcessJobAsync(job.ID, req.Content)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"job_id": job.ID,
			"status": job.Status,
		})
	}
}

// --- GET /pantry/ingest/:job_id ---

func handleGetJob(ingest *service.IngestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
		if err != nil {
			jsonError(w, "invalid job_id", http.StatusBadRequest)
			return
		}

		job, err := ingest.GetJob(r.Context(), jobID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "job not found", http.StatusNotFound)
				return
			}
			jsonError(w, "failed to get job", http.StatusInternalServerError, err)
			return
		}

		items, err := ingest.ListStagedItems(r.Context(), jobID)
		if err != nil {
			jsonError(w, "failed to get staged items", http.StatusInternalServerError, err)
			return
		}

		jsonOK(w, map[string]any{
			"job_id": job.ID,
			"status": job.Status,
			"items":  items,
		})
	}
}

// --- POST /pantry/ingest/:job_id/confirm ---

type confirmRequest struct {
	Overrides []service.OverrideItem `json:"overrides"`
}

func handleConfirmJob(pantry *service.PantryService, ingest *service.IngestService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobID, err := uuid.Parse(chi.URLParam(r, "job_id"))
		if err != nil {
			jsonError(w, "invalid job_id", http.StatusBadRequest)
			return
		}

		var req confirmRequest
		// body is optional — decode only if present
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, "invalid request body", http.StatusBadRequest)
				return
			}
		}

		if err := ingest.ConfirmJob(r.Context(), jobID, pantry, req.Overrides); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				jsonError(w, "job not found", http.StatusNotFound)
				return
			}
			jsonError(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
