package service

import (
	"context"

	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
)

// DictionaryResolver abstracts the Dictionary client for testing.
type DictionaryResolver interface {
	Resolve(ctx context.Context, rawName string) (clients.ResolveResult, error)
}

// LLMExtractor abstracts LLM-based text extraction for testing.
type LLMExtractor interface {
	Extract(ctx context.Context, text string) (*ExtractionResponse, error)
}
