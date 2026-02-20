package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// DictionaryClient calls the Ingredient Dictionary service.
type DictionaryClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewDictionaryClient(baseURL string, httpClient *http.Client) *DictionaryClient {
	return &DictionaryClient{baseURL: baseURL, httpClient: httpClient}
}

// ResolveResult is the response from POST /ingredients/resolve.
type ResolveResult struct {
	Ingredient struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	} `json:"ingredient"`
	Confidence float64 `json:"confidence"`
	Created    bool    `json:"created"`
}

// Resolve calls the Dictionary service to normalize rawName to a canonical ID.
func (c *DictionaryClient) Resolve(ctx context.Context, rawName string) (ResolveResult, error) {
	body, err := json.Marshal(map[string]string{"name": rawName})
	if err != nil {
		return ResolveResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ingredients/resolve", bytes.NewReader(body))
	if err != nil {
		return ResolveResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ResolveResult{}, fmt.Errorf("dictionary resolve: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return ResolveResult{}, fmt.Errorf("dictionary resolve: unexpected status %d", resp.StatusCode)
	}

	var result ResolveResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ResolveResult{}, fmt.Errorf("dictionary resolve decode: %w", err)
	}
	return result, nil
}
