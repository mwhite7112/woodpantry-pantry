package db

import (
	"database/sql/driver"

	"github.com/google/uuid"
)

// NullUUID represents a UUID that may be null. Used for optional FK references
// such as staged_items.ingredient_id which is null when a Dictionary match was
// not found.
//
//nolint:recvcheck // Scan requires a pointer receiver to mutate; Value uses a value receiver so NullUUID itself satisfies driver.Valuer
type NullUUID struct {
	UUID  uuid.UUID
	Valid bool
}

func (n *NullUUID) Scan(value any) error {
	if value == nil {
		n.UUID = uuid.UUID{}
		n.Valid = false
		return nil
	}
	n.Valid = true
	return n.UUID.Scan(value)
}

func (n NullUUID) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil //nolint:nilnil // nil driver.Value is the correct representation of SQL NULL
	}
	return n.UUID.Value()
}
