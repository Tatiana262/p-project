package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"parser-project/internal/core/domain"
	"parser-project/internal/core/usecase"
	"parser-project/pkg/rabbitmq/rabbitmq_consumer"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

// LinkConsumerAdapter - это входящий адаптер, который слушает очередь
// со ссылками и вызывает use case для их обработки.
type LinkConsumerAdapter struct {
	consumer   *rabbitmq_consumer.Consumer
	useCase    *usecase.ProcessLinkUseCase // Зависимость от конкретного UseCase
}

// NewLinkConsumerAdapter создает новый адаптер.
func NewLinkConsumerAdapter(
	consumerCfg rabbitmq_consumer.ConsumerConfig,
	useCase *usecase.ProcessLinkUseCase,
) (*LinkConsumerAdapter, error) {
	
	adapter := &LinkConsumerAdapter{
		useCase: useCase,
	}

	// Создаем consumer, передавая ему метод этого адаптера как обработчик.
	// Теперь `messageHandler` является частью адаптера, а не App.
	consumer, err := rabbitmq_consumer.NewConsumer(consumerCfg, adapter.messageHandler)
	if err != nil {
		return nil, fmt.Errorf("failed to create RabbitMQ consumer for links: %w", err)
	}
	adapter.consumer = consumer

	return adapter, nil
}

// messageHandler - приватный метод адаптера.
func (a *LinkConsumerAdapter) messageHandler(d amqp.Delivery) (ack bool, requeueOnError bool, err error) {
	log.Printf("LinkConsumerAdapter: Received task (Tag: %d)\n", d.DeliveryTag)

	var linkToParse domain.PropertyLink
	if err := json.Unmarshal(d.Body, &linkToParse); err != nil {
		log.Printf("LinkConsumerAdapter: Error unmarshalling: %v. NACK (no requeue).\n", err)
		return false, false, fmt.Errorf("unmarshal error: %w", err)
	}

	if d.Redelivered {
        log.Printf("LinkConsumerAdapter: Message (Tag: %d) is redelivered. Possible poison pill.", d.DeliveryTag)
        // Здесь можно добавить логику: если это уже 3-я попытка, то не делать requeue.
    }

	// Адаптер вызывает UseCase
	err = a.useCase.Execute(context.Background(), linkToParse)
	if err != nil {
		// Если это ошибка 429 И сообщение уже было доставлено повторно
        if strings.Contains(err.Error(), "Too Many Requests") && d.Redelivered {
			log.Printf("LinkConsumerAdapter: Repeated 'Too Many Requests' error. Discarding message (Tag: %d).", d.DeliveryTag)
			return false, false, err // NACK без requeue! Отправляем в "мертвую" очередь, если она есть.
	    }
	   
	   log.Printf("LinkConsumerAdapter: Use case failed: %v. Requeueing task.", err)
	   return false, true, err // Первая попытка - можно и requeue
	}

	return true, false, nil
}

// Start реализует EventListenerPort
func (a *LinkConsumerAdapter) Start(ctx context.Context) error {
	return a.consumer.StartConsuming(ctx)
}

// Close реализует EventListenerPort
func (a *LinkConsumerAdapter) Close() error {
	return a.consumer.Close()
}