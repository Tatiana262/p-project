package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"parser-project/internal/core/domain"
	"parser-project/pkg/rabbitmq/rabbitmq_producer"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQProcessedPropertyQueueAdapter для отправки обработанных объектов
type RabbitMQProcessedPropertyQueueAdapter struct {
	producer   *rabbitmq_producer.Publisher
	routingKey string
}

// NewRabbitMQProcessedPropertyQueueAdapter создает новый экземпляр
func NewRabbitMQProcessedPropertyQueueAdapter(producer *rabbitmq_producer.Publisher, routingKey string) (*RabbitMQProcessedPropertyQueueAdapter, error) {
	if producer == nil { return nil, fmt.Errorf("producer cannot be nil") }
	if routingKey == "" { return nil, fmt.Errorf("routingKey cannot be empty") }
	return &RabbitMQProcessedPropertyQueueAdapter{
		producer:   producer,
		routingKey: routingKey,
	}, nil
}

// Enqueue отправляет PropertyRecord в очередь
func (a *RabbitMQProcessedPropertyQueueAdapter) Enqueue(ctx context.Context, record domain.PropertyRecord) error {
	recordJSON, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal property record to JSON for URL %s: %w", record.Source, err)
	}

	msg := amqp.Publishing{
		ContentType:  "application/json",
		Body:         recordJSON,
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
	}

	publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	log.Printf("ProcessedPropertyQueue: Publishing processed record for URL '%s' to key '%s'\n", record.Source, a.routingKey)
	return a.producer.Publish(publishCtx, a.routingKey, msg)
}