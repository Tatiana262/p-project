package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"parser-project/internal/core/domain"
	"parser-project/pkg/rabbitmq/rabbitmq_producer" // Путь к вашему пакету продюсера
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQLinkQueueAdapter реализует интерфейс PropertyLinkQueuePort для RabbitMQ.
type RabbitMQLinkQueueAdapter struct {
	producer   *rabbitmq_producer.Publisher
	routingKey string // Ключ маршрутизации для отправки ссылок
	// Можно добавить ExchangeName, если он не задан глобально в producer'е
	// exchangeName string
}

// NewRabbitMQLinkQueueAdapter создает новый экземпляр RabbitMQLinkQueueAdapter.
// producer - это уже инициализированный экземпляр вашего rabbitmq_producer.Publisher.
// routingKey - ключ, с которым будут публиковаться сообщения (например, "link.task.kufar").
func NewRabbitMQLinkQueueAdapter(producer *rabbitmq_producer.Publisher, routingKey string) (*RabbitMQLinkQueueAdapter, error) {
	if producer == nil {
		return nil, fmt.Errorf("rabbitmq adapter: producer cannot be nil")
	}
	if routingKey == "" {
		return nil, fmt.Errorf("rabbitmq adapter: routingKey cannot be empty")
	}
	return &RabbitMQLinkQueueAdapter{
		producer:   producer,
		routingKey: routingKey,
	}, nil
}

// Enqueue отправляет ссылку в очередь RabbitMQ.
func (a *RabbitMQLinkQueueAdapter) Enqueue(ctx context.Context, link domain.PropertyLink) error {
	// Сериализуем структуру PropertyLink в JSON для отправки
	linkJSON, err := json.Marshal(link)
	if err != nil {
		return fmt.Errorf("rabbitmq adapter: failed to marshal property link to JSON for URL %s: %w", link.URL, err)
	}

	msg := amqp.Publishing{
		ContentType:  "application/json", // Указываем, что отправляем JSON
		Body:         linkJSON,
		DeliveryMode: amqp.Persistent, // Для сохранения сообщений при перезапуске брокера
		Timestamp:    time.Now(),
		// Можно добавить AppId или другие свойства, если необходимо
		// AppId: "parser-project",
	}

	// Устанавливаем таймаут на операцию публикации, если контекст его не предоставляет
	publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second) // Таймаут 10 секунд на публикацию
	defer cancel()

	log.Printf("RabbitMQAdapter: Publishing link to routing key '%s': %s\n", a.routingKey, link.URL)
	err = a.producer.Publish(publishCtx, a.routingKey, msg)
	if err != nil {
		return fmt.Errorf("rabbitmq adapter: failed to publish property link %s: %w", link.URL, err)
	}

	log.Printf("RabbitMQAdapter: Successfully published link: %s\n", link.URL)
	return nil
}