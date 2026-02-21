package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
