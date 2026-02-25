package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/mocks"
)

type stubUpdatePublisher struct {
	err       error
	published [][]uuid.UUID
}

func (s *stubUpdatePublisher) PublishPantryUpdated(_ context.Context, ids []uuid.UUID) error {
	cloned := append([]uuid.UUID(nil), ids...)
	s.published = append(s.published, cloned)
	return s.err
}

func TestListItems_ReturnsItems(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	svc := NewPantryService(mockQ)

	now := time.Now()
	expected := []db.PantryItem{
		{
			ID:           uuid.New(),
			IngredientID: uuid.New(),
			Quantity:     2.0,
			Unit:         "cup",
			ExpiresAt:    sql.NullTime{},
			AddedAt:      now,
			UpdatedAt:    now,
		},
		{
			ID:           uuid.New(),
			IngredientID: uuid.New(),
			Quantity:     1.0,
			Unit:         "lb",
			ExpiresAt:    sql.NullTime{},
			AddedAt:      now,
			UpdatedAt:    now,
		},
	}

	mockQ.EXPECT().ListPantryItems(mock.Anything).Return(expected, nil)

	items, err := svc.ListItems(context.Background())
	require.NoError(t, err)
	assert.Equal(t, expected, items)
}

func TestListItems_ReturnsEmptySliceOnNil(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	svc := NewPantryService(mockQ)

	mockQ.EXPECT().ListPantryItems(mock.Anything).Return(nil, nil)

	items, err := svc.ListItems(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, items)
	assert.Empty(t, items)
}

func TestUpsertItem_DelegatesToQuerier(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	svc := NewPantryService(mockQ)

	ingredientID := uuid.New()
	now := time.Now()
	expected := db.PantryItem{
		ID:           uuid.New(),
		IngredientID: ingredientID,
		Quantity:     3.5,
		Unit:         "oz",
		ExpiresAt:    sql.NullTime{},
		AddedAt:      now,
		UpdatedAt:    now,
	}

	mockQ.EXPECT().UpsertPantryItem(mock.Anything, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     3.5,
		Unit:         "oz",
		ExpiresAt:    sql.NullTime{},
	}).Return(expected, nil)

	item, err := svc.UpsertItem(context.Background(), ingredientID, 3.5, "oz", sql.NullTime{})
	require.NoError(t, err)
	assert.Equal(t, expected, item)
}

func TestDeleteItem_DelegatesToQuerier(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	svc := NewPantryService(mockQ)

	id := uuid.New()
	mockQ.EXPECT().DeletePantryItem(mock.Anything, id).Return(nil)

	err := svc.DeleteItem(context.Background(), id)
	require.NoError(t, err)
}

func TestReset_DelegatesToDeleteAllPantryItems(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	svc := NewPantryService(mockQ)

	mockQ.EXPECT().DeleteAllPantryItems(mock.Anything).Return(nil)

	err := svc.Reset(context.Background())
	require.NoError(t, err)
}

func TestUpsertItem_PublishFailureDoesNotFailRequest(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	pub := &stubUpdatePublisher{err: errors.New("rabbitmq unavailable")}
	svc := NewPantryService(mockQ, pub)

	ingredientID := uuid.New()
	itemID := uuid.New()
	now := time.Now()

	mockQ.EXPECT().UpsertPantryItem(mock.Anything, db.UpsertPantryItemParams{
		IngredientID: ingredientID,
		Quantity:     2.0,
		Unit:         "cup",
		ExpiresAt:    sql.NullTime{},
	}).Return(db.PantryItem{
		ID:           itemID,
		IngredientID: ingredientID,
		Quantity:     2.0,
		Unit:         "cup",
		AddedAt:      now,
		UpdatedAt:    now,
	}, nil)

	item, err := svc.UpsertItem(context.Background(), ingredientID, 2.0, "cup", sql.NullTime{})
	require.NoError(t, err)
	assert.Equal(t, itemID, item.ID)
	require.Len(t, pub.published, 1)
	assert.Equal(t, []uuid.UUID{itemID}, pub.published[0])
}

func TestDeleteItem_PublishFailureDoesNotFailRequest(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	pub := &stubUpdatePublisher{err: errors.New("rabbitmq unavailable")}
	svc := NewPantryService(mockQ, pub)

	itemID := uuid.New()
	mockQ.EXPECT().DeletePantryItem(mock.Anything, itemID).Return(nil)

	err := svc.DeleteItem(context.Background(), itemID)
	require.NoError(t, err)
	require.Len(t, pub.published, 1)
	assert.Equal(t, []uuid.UUID{itemID}, pub.published[0])
}

func TestReset_PublishFailureDoesNotFailRequest(t *testing.T) {
	t.Parallel()

	mockQ := mocks.NewMockQuerier(t)
	pub := &stubUpdatePublisher{err: errors.New("rabbitmq unavailable")}
	svc := NewPantryService(mockQ, pub)

	mockQ.EXPECT().DeleteAllPantryItems(mock.Anything).Return(nil)

	err := svc.Reset(context.Background())
	require.NoError(t, err)
	require.Len(t, pub.published, 1)
	assert.Empty(t, pub.published[0])
}
