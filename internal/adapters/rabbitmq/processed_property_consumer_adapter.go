package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"parser-project/internal/core/domain"
	"parser-project/internal/core/usecase"
	"parser-project/pkg/rabbitmq/rabbitmq_consumer"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ProcessedPropertyConsumerAdapter - это входящий адаптер, который слушает очередь
// с обработанными объектами недвижимости и вызывает use case для их сохранения.
type ProcessedPropertyConsumerAdapter struct {
	consumer *rabbitmq_consumer.Consumer
	useCase  *usecase.SavePropertyUseCase // Зависимость от конкретного UseCase
}

// NewProcessedPropertyConsumerAdapter создает новый адаптер.
func NewProcessedPropertyConsumerAdapter(
	consumerCfg rabbitmq_consumer.ConsumerConfig,
	useCase *usecase.SavePropertyUseCase,
) (*ProcessedPropertyConsumerAdapter, error) {

	adapter := &ProcessedPropertyConsumerAdapter{
		useCase: useCase,
	}

	// Создаем consumer, передавая ему метод этого адаптера как обработчик.
	// consumer, err := rabbitmq_consumer.NewConsumer(consumerCfg, adapter.messageHandler)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create RabbitMQ consumer for processed properties: %w", err)
	// }
	// adapter.consumer = consumer

	return adapter, nil
}

// messageHandler - приватный метод адаптера, который обрабатывает входящие сообщения.
func (a *ProcessedPropertyConsumerAdapter) messageHandler(d amqp.Delivery) (ack bool, requeueOnError bool, err error) {
	log.Printf("ProcessedPropertyConsumerAdapter: Received data (Tag: %d)\n", d.DeliveryTag)

	var propertyRecord domain.PropertyRecord
	if err := json.Unmarshal(d.Body, &propertyRecord); err != nil {
		log.Printf("ProcessedPropertyConsumerAdapter: Error unmarshalling: %v. NACK (no requeue).\n", err)
		return false, false, fmt.Errorf("unmarshal error: %w", err)
	}

	// Адаптер вызывает UseCase для сохранения данных
	err = a.useCase.Execute(context.Background(), propertyRecord)
	if err != nil {
		log.Printf("ProcessedPropertyConsumerAdapter: Use case failed: %v. Requeueing message.\n", err)
		// Если сохранение не удалось (например, проблема с файловой системой),
		// возвращаем сообщение в очередь для повторной попытки.
		return false, true, err
	}

	log.Printf("ProcessedPropertyConsumerAdapter: Record successfully processed (Tag: %d).\n", d.DeliveryTag)
	return true, false, nil
}

// Start реализует EventListenerPort, запуская прослушивание очереди.
func (a *ProcessedPropertyConsumerAdapter) Start(ctx context.Context) error {
	return nil
	// return a.consumer.StartConsuming(ctx)
}

// Close реализует EventListenerPort, корректно останавливая консьюмера.
func (a *ProcessedPropertyConsumerAdapter) Close() error {
	return nil
	// return a.consumer.Close()
}