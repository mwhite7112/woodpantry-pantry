package db

import (
	"database/sql/driver"

	"github.com/google/uuid"
)

// NullUUID represents a UUID that may be null. Used for optional FK references
// such as staged_items.ingredient_id which is null when a Dictionary match was
// not found.
type NullUUID struct {
	UUID  uuid.UUID
	Valid bool
}

func (n *NullUUID) Scan(value interface{}) error {
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
		return nil, nil
	}
	return n.UUID.Value()
}
