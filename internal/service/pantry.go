package service

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
)

// PantryService handles pantry item CRUD.
type PantryService struct {
	q db.Querier
}

func NewPantryService(q db.Querier) *PantryService {
	return &PantryService{q: q}
}

func (s *PantryService) ListItems(ctx context.Context) ([]db.PantryItem, error) {
	items, err := s.q.ListPantryItems(ctx)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return []db.PantryItem{}, nil
	}
	return items, nil
}

func (s *PantryService) UpsertItem(ctx context.Context, ingredientID uuid.UUID, quantity float64, unit string, expiresAt sql.NullTime) (db.PantryItem, error) {
	return s.q.UpsertPantryItem(ctx, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     quantity,
		Unit:         unit,
		ExpiresAt:    expiresAt,
	})
}

func (s *PantryService) DeleteItem(ctx context.Context, id uuid.UUID) error {
	return s.q.DeletePantryItem(ctx, id)
}

func (s *PantryService) Reset(ctx context.Context) error {
	return s.q.DeleteAllPantryItems(ctx)
}
