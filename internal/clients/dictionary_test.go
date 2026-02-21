package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_Success200(t *testing.T) {
	t.Parallel()

	ingredientID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/ingredients/resolve", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "garlic", body["name"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ResolveResult{
			Ingredient: struct {
				ID   uuid.UUID `json:"id"`
				Name string    `json:"name"`
			}{ID: ingredientID, Name: "garlic"},
			Confidence: 0.95,
			Created:    false,
		})
	}))
	defer server.Close()

	client := NewDictionaryClient(server.URL, server.Client())
	result, err := client.Resolve(context.Background(), "garlic")

	require.NoError(t, err)
	assert.Equal(t, ingredientID, result.Ingredient.ID)
	assert.Equal(t, "garlic", result.Ingredient.Name)
	assert.Equal(t, 0.95, result.Confidence)
	assert.False(t, result.Created)
}

func TestResolve_Success201Created(t *testing.T) {
	t.Parallel()

	ingredientID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ResolveResult{
			Ingredient: struct {
				ID   uuid.UUID `json:"id"`
				Name string    `json:"name"`
			}{ID: ingredientID, Name: "quinoa"},
			Confidence: 1.0,
			Created:    true,
		})
	}))
	defer server.Close()

	client := NewDictionaryClient(server.URL, server.Client())
	result, err := client.Resolve(context.Background(), "quinoa")

	require.NoError(t, err)
	assert.Equal(t, ingredientID, result.Ingredient.ID)
	assert.True(t, result.Created)
}

func TestResolve_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewDictionaryClient(server.URL, server.Client())
	_, err := client.Resolve(context.Background(), "garlic")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestResolve_InvalidJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json{{{"))
	}))
	defer server.Close()

	client := NewDictionaryClient(server.URL, server.Client())
	_, err := client.Resolve(context.Background(), "garlic")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}
