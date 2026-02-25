package service

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mwhite7112/woodpantry-pantry/internal/db"
)

// UpdatePublisher publishes pantry.updated events after stock changes.
type UpdatePublisher interface {
	PublishPantryUpdated(ctx context.Context, changedItemIDs []uuid.UUID) error
}

// PantryService handles pantry item CRUD.
type PantryService struct {
	q         db.Querier
	publisher UpdatePublisher
}

func NewPantryService(q db.Querier, publishers ...UpdatePublisher) *PantryService {
	publisher := UpdatePublisher(noopUpdatePublisher{})
	if len(publishers) > 0 && publishers[0] != nil {
		publisher = publishers[0]
	}

	return &PantryService{
		q:         q,
		publisher: publisher,
	}
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

func (s *PantryService) UpsertItem(
	ctx context.Context,
	ingredientID uuid.UUID,
	quantity float64,
	unit string,
	expiresAt sql.NullTime,
) (db.PantryItem, error) {
	item, err := s.UpsertItemNoPublish(ctx, ingredientID, quantity, unit, expiresAt)
	if err != nil {
		return db.PantryItem{}, err
	}

	s.publishPantryUpdated(ctx, []uuid.UUID{item.ID})
	return item, nil
}

func (s *PantryService) UpsertItemNoPublish(
	ctx context.Context,
	ingredientID uuid.UUID,
	quantity float64,
	unit string,
	expiresAt sql.NullTime,
) (db.PantryItem, error) {
	return s.q.UpsertPantryItem(ctx, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     quantity,
		Unit:         unit,
		ExpiresAt:    expiresAt,
	})
}

func (s *PantryService) DeleteItem(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeletePantryItem(ctx, id); err != nil {
		return err
	}

	s.publishPantryUpdated(ctx, []uuid.UUID{id})
	return nil
}

func (s *PantryService) Reset(ctx context.Context) error {
	if err := s.q.DeleteAllPantryItems(ctx); err != nil {
		return err
	}

	// The reset operation affects all items; emit an empty changed_item_ids list.
	s.publishPantryUpdated(ctx, []uuid.UUID{})
	return nil
}

func (s *PantryService) PublishUpdated(ctx context.Context, changedItemIDs []uuid.UUID) {
	s.publishPantryUpdated(ctx, changedItemIDs)
}

func (s *PantryService) publishPantryUpdated(ctx context.Context, changedItemIDs []uuid.UUID) {
	if err := s.publisher.PublishPantryUpdated(ctx, changedItemIDs); err != nil {
		slog.Default().WarnContext(
			ctx,
			"failed to publish pantry.updated",
			"changed_item_ids",
			changedItemIDs,
			"error",
			err,
		)
	}
}

type noopUpdatePublisher struct{}

func (noopUpdatePublisher) PublishPantryUpdated(_ context.Context, _ []uuid.UUID) error {
	return nil
}
