package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	exchangeName = "woodpantry.topic"
	routingKey   = "pantry.updated"
)

// PantryUpdatedPublisher publishes pantry.updated events.
type PantryUpdatedPublisher struct {
	conn *amqp.Connection
}

type pantryUpdatedEvent struct {
	Timestamp      string      `json:"timestamp"`
	ChangedItemIDs []uuid.UUID `json:"changed_item_ids"`
}

// NewPantryUpdatedPublisher creates a RabbitMQ publisher and ensures the
// shared topic exchange exists.
func NewPantryUpdatedPublisher(rabbitmqURL string) (*PantryUpdatedPublisher, error) {
	conn, err := amqp.Dial(rabbitmqURL)
	if err != nil {
		return nil, fmt.Errorf("connect rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	if err := ch.ExchangeDeclare(
		exchangeName,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("declare exchange %q: %w", exchangeName, err)
	}

	return &PantryUpdatedPublisher{conn: conn}, nil
}

// PublishPantryUpdated publishes the minimal pantry.updated payload.
func (p *PantryUpdatedPublisher) PublishPantryUpdated(
	ctx context.Context,
	changedItemIDs []uuid.UUID,
) error {
	ch, err := p.conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	event := pantryUpdatedEvent{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		ChangedItemIDs: changedItemIDs,
	}
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal pantry.updated event: %w", err)
	}

	if err := ch.PublishWithContext(ctx, exchangeName, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("publish pantry.updated: %w", err)
	}

	return nil
}

// Close closes the RabbitMQ connection.
func (p *PantryUpdatedPublisher) Close() error {
	return p.conn.Close()
}
