//go:build integration

package service

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPantry_UpsertIdempotent(t *testing.T) {
	sqlDB := testutil.SetupDB(t)
	q := db.New(sqlDB)
	svc := NewPantryService(q)
	ctx := context.Background()

	ingID := uuid.New()

	// First upsert creates the item.
	item1, err := svc.UpsertItem(ctx, ingID, 2.0, "cup", sql.NullTime{})
	require.NoError(t, err)
	assert.Equal(t, 2.0, item1.Quantity)
	assert.Equal(t, "cup", item1.Unit)

	// Second upsert updates quantity.
	item2, err := svc.UpsertItem(ctx, ingID, 5.0, "cup", sql.NullTime{})
	require.NoError(t, err)
	assert.Equal(t, 5.0, item2.Quantity)
	assert.Equal(t, item1.ID, item2.ID) // Same row, not a new one.

	// Verify only one item in pantry.
	items, err := svc.ListItems(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 1)
}

func TestPantry_DeleteItem(t *testing.T) {
	sqlDB := testutil.SetupDB(t)
	q := db.New(sqlDB)
	svc := NewPantryService(q)
	ctx := context.Background()

	ingID := uuid.New()
	item, err := svc.UpsertItem(ctx, ingID, 1.0, "lb", sql.NullTime{})
	require.NoError(t, err)

	err = svc.DeleteItem(ctx, item.ID)
	require.NoError(t, err)

	items, err := svc.ListItems(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 0)
}

func TestPantry_Reset(t *testing.T) {
	sqlDB := testutil.SetupDB(t)
	q := db.New(sqlDB)
	svc := NewPantryService(q)
	ctx := context.Background()

	// Add several items.
	for i := 0; i < 5; i++ {
		_, err := svc.UpsertItem(ctx, uuid.New(), float64(i+1), "piece", sql.NullTime{})
		require.NoError(t, err)
	}

	items, err := svc.ListItems(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 5)

	err = svc.Reset(ctx)
	require.NoError(t, err)

	items, err = svc.ListItems(ctx)
	require.NoError(t, err)
	assert.Len(t, items, 0)
}
