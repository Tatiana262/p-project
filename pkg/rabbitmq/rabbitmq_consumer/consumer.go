package rabbitmq_consumer

import (
	"context"
	"fmt"
	"log"
	"parser-project/pkg/rabbitmq/rabbitmq_common"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// MessageHandler функция-обработчик для полученных сообщений
type MessageHandler func(delivery amqp.Delivery) (ack bool, requeueOnError bool, err error)

// ConsumerConfig конфигурация для потребителя
type ConsumerConfig struct {
	rabbitmq_common.Config
	// Настройки очереди
	QueueName             string // Имя очереди для потребления (если пусто, имя будет сгенерировано сервером)
	DeclareQueue          bool   // Пытаться ли объявить очередь
	DurableQueue          bool
	ExclusiveQueue        bool
	AutoDeleteQueue       bool
	QueueArgs             amqp.Table // Дополнительные аргументы для очереди (например, x-message-ttl, x-dead-letter-exchange)

	// Настройки обменника (если нужно объявлять или привязываться к нему)
	ExchangeNameForBind   string // Имя обменника для привязки очереди (если пусто, привязка не выполняется)
	DeclareExchangeForBind bool  // Пытаться ли объявить этот обменник
	ExchangeTypeForBind   string // Тип этого обменника
	DurableExchangeForBind bool
	ExchangeArgsForBind   amqp.Table // Аргументы для обменника, если объявляем его

	// Настройки привязки
	RoutingKeyForBind     string     // Ключ маршрутизации для привязки
	BindingArgs           amqp.Table // Дополнительные аргументы для привязки

	// Настройки QoS
	PrefetchCount         int // 0 или меньше - без ограничений
	PrefetchSize          int // 0 - без ограничений
	QosGlobal             bool

	// Настройки потребителя
	ConsumerTag           string // Тег потребителя (если пустой, генерируется RabbitMQ)
	ExclusiveConsumer     bool
}

// Consumer структура для управления потребителем
type Consumer struct {
	config     ConsumerConfig
	handler    MessageHandler
	connection *amqp.Connection
	channel    *amqp.Channel
	// Для хранения имени очереди, особенно если оно генерируется сервером
	actualQueueName string

	// Для управления graceful shutdown
	wg         sync.WaitGroup
	// cancelFunc context.CancelFunc // Для отмены потребления
}

// NewConsumer создает нового потребителя
func NewConsumer(cfg ConsumerConfig, handler MessageHandler) (*Consumer, error) {
	if err := cfg.Validate(); err != nil { // Валидация общей части
		return nil, fmt.Errorf("invalid base config: %w", err)
	}
	// Валидация специфичная для ConsumerConfig
	if !cfg.DeclareQueue && cfg.QueueName == "" {
		return nil, fmt.Errorf("consumer: queue name is required if DeclareQueue is false")
	}
	if cfg.ExchangeNameForBind != "" && cfg.ExchangeTypeForBind == "" && cfg.DeclareExchangeForBind {
		return nil, fmt.Errorf("consumer: exchange type is required if declaring an exchange for binding")
	}
	if handler == nil {
		return nil, fmt.Errorf("consumer: message handler is required")
	}

	c := &Consumer{
		config:  cfg,
		handler: handler,
	}

	if err := c.connectAndSetup(); err != nil {
		return nil, fmt.Errorf("consumer: initial connection and setup failed: %w", err)
	}

	return c, nil
}

// connectAndSetup устанавливает соединение, канал и настраивает сущности RabbitMQ
func (c *Consumer) connectAndSetup() error {
	log.Printf("Consumer: Attempting to connect to RabbitMQ at %s\n", c.config.URL)
	conn, err := amqp.Dial(c.config.URL)
	if err != nil {
		return fmt.Errorf("failed to dial RabbitMQ: %w", err)
	}
	c.connection = conn

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to open a channel: %w", err)
	}
	c.channel = ch
	log.Println("Consumer: Channel opened.")

	// 1. Настройка QoS (должна быть до Consume)
	if c.config.PrefetchCount > 0 || c.config.PrefetchSize > 0 {
		log.Printf("Consumer: Setting QoS (PrefetchCount: %d, PrefetchSize: %d, Global: %v)\n",
			c.config.PrefetchCount, c.config.PrefetchSize, c.config.QosGlobal)
		err = c.channel.Qos(
			c.config.PrefetchCount,
			c.config.PrefetchSize,
			c.config.QosGlobal,
		)
		if err != nil {
			_ = c.channel.Close()
			_ = c.connection.Close()
			return fmt.Errorf("failed to set QoS: %w", err)
		}
	}

	c.actualQueueName = c.config.QueueName
	// 2. Объявление очереди (если нужно)
	if c.config.DeclareQueue {
		log.Printf("Consumer: Declaring queue '%s' (durable: %v, exclusive: %v, autoDelete: %v)\n",
			c.config.QueueName, c.config.DurableQueue, c.config.ExclusiveQueue, c.config.AutoDeleteQueue)
		q, declareErr := c.channel.QueueDeclare(
			c.config.QueueName,       // name 
			c.config.DurableQueue,    // durable
			c.config.AutoDeleteQueue, // delete when unused
			c.config.ExclusiveQueue,  // exclusive
			false,                    // no-wait
			c.config.QueueArgs,       // arguments
		)
		if declareErr != nil {
			_ = c.channel.Close()
			_ = c.connection.Close()
			return fmt.Errorf("failed to declare queue '%s': %w", c.config.QueueName, declareErr)
		}
		c.actualQueueName = q.Name // Используем имя, возвращенное сервером 
	} 

	// 3. Объявление обменника (если нужно для привязки)
	if c.config.DeclareExchangeForBind {
		log.Printf("Consumer: Declaring exchange '%s' for binding (type: %s, durable: %v)\n",
			c.config.ExchangeNameForBind, c.config.ExchangeTypeForBind, c.config.DurableExchangeForBind)
		err = c.channel.ExchangeDeclare(
			c.config.ExchangeNameForBind,
			c.config.ExchangeTypeForBind,
			c.config.DurableExchangeForBind,
			false, // auto-deleted
			false, // internal
			false, // no-wait
			c.config.ExchangeArgsForBind,
		)
		if err != nil {
			_ = c.channel.Close()
			_ = c.connection.Close()
			return fmt.Errorf("failed to declare exchange '%s' for binding: %w", c.config.ExchangeNameForBind, err)
		}
	}

	// 4. Привязка очереди к обменнику (если нужно)
	if c.config.ExchangeNameForBind != "" {
		log.Printf("Consumer: Binding queue '%s' to exchange '%s' with routing key '%s'\n",
			c.actualQueueName, c.config.ExchangeNameForBind, c.config.RoutingKeyForBind)
		err = c.channel.QueueBind(
			c.actualQueueName,
			c.config.RoutingKeyForBind,
			c.config.ExchangeNameForBind,
			false, // noWait
			c.config.BindingArgs,
		)
		if err != nil {
			_ = c.channel.Close()
			_ = c.connection.Close()
			return fmt.Errorf("failed to bind queue '%s' to exchange '%s': %w", c.actualQueueName, c.config.ExchangeNameForBind, err)
		}
	}

	log.Printf("Consumer: Setup complete for queue '%s'.\n", c.actualQueueName)
	return nil
}


// StartConsuming начинает потребление сообщений
func (c *Consumer) StartConsuming(ctx context.Context) error {
	if c.channel == nil || c.connection == nil || c.connection.IsClosed() {
		return fmt.Errorf("consumer: not connected. Please create a new consumer or ensure connection is stable")
	}

	// Создаем контекст, который мы можем отменить, чтобы остановить горутины
	// ctx, cancel := context.WithCancel(context.Background())
	// c.cancelFunc = cancel

	msgs, err := c.channel.Consume(
		c.actualQueueName,     // Используем актуальное имя очереди
		c.config.ConsumerTag,
		false,                 // auto-ack
		c.config.ExclusiveConsumer, 
		false,                 // no-local
		false,                 // no-wait
		nil,                   // args
	)
	if err != nil {
		return fmt.Errorf("consumer: failed to register a consumer on queue '%s': %w", c.actualQueueName, err)
	}

	log.Printf("Consumer: [*] Waiting for messages on queue '%s'.\n", c.actualQueueName)

	// Запускаем горутину, которая будет читать из канала RabbitMQ и распределять работу
	go func() {
		for {
			// --- Шаг 1: Приоритетная, неблокирующая проверка на отмену ---
			// Это гарантирует, что мы не запустим нового "работника", если уже получили команду на остановку.
			select {
			case <-ctx.Done():
				log.Printf("Consumer: (Priority Check) Context cancelled for tag '%s'. Exiting consumption loop.", c.config.ConsumerTag)
				return // Выходим из горутины-диспетчера
			default:
				// Контекст не отменен, продолжаем.
			}

			// --- Шаг 2: Блокирующее ожидание нового сообщения ИЛИ отмены ---
			// Этот select ждет, пока что-то произойдет.
			select {
			case <-ctx.Done(): // Если контекст был отменен (например, при вызове Close)
				// Эта ветка сработает, если контекст отменили, ПОКА мы ждали сообщение.
				log.Printf("Consumer: (Wait Check) Context cancelled for tag '%s'. Exiting consumption loop.", c.config.ConsumerTag)
				return // Выходим из горутины-диспетчера
				
			case d, ok := <-msgs:
				if !ok {
					log.Printf("Consumer: Deliveries channel closed by RabbitMQ for tag '%s'. Exiting loop.", c.config.ConsumerTag)
					return
				}
				
				// Запускаем обработчик для каждого сообщения в новой горутине
				c.wg.Add(1) // Увеличиваем счетчик WaitGroup
				go func(delivery amqp.Delivery) {
					defer c.wg.Done() // Уменьшаем счетчик, когда горутина завершается

					log.Printf("Consumer: [->] Started processing message (Tag: %d)\n", delivery.DeliveryTag)
					
					ack, requeueOnError, processErr := c.handler(delivery)
					
					// ... (остальная логика ack/nack остается точно такой же) ...
					if processErr != nil {
						log.Printf("Consumer: Error processing message (Tag: %d): %v. Requeue: %v\n", delivery.DeliveryTag, processErr, requeueOnError)
						if err := delivery.Nack(false, requeueOnError); err != nil {
							log.Printf("Consumer: Error sending Nack (Tag: %d): %v\n", delivery.DeliveryTag, err)
						}
					} else {
						if ack {
							if err := delivery.Ack(false); err != nil {
								log.Printf("Consumer: Error sending Ack (Tag: %d): %v\n", delivery.DeliveryTag, err)
							} else {
								log.Printf("Consumer: [+] Message Ack'd (Tag: %d)\n", delivery.DeliveryTag)
							}
						} else {
							log.Printf("Consumer: [-] Message Nack'd (no requeue) by handler (Tag: %d)\n", delivery.DeliveryTag)
							if err := delivery.Nack(false, false); err != nil {
								log.Printf("Consumer: Error sending Nack (no requeue) (Tag: %d): %v\n", delivery.DeliveryTag, err)
							}
						}
					}
					log.Printf("Consumer: [<-] Finished processing message (Tag: %d)\n", delivery.DeliveryTag)
				}(d)
			}
		}
	}()

	// Ждем, пока соединение не будет закрыто
	notifyClose := make(chan *amqp.Error)
	c.connection.NotifyClose(notifyClose)
	
	// Теперь мы ждем либо отмены внешнего контекста, либо закрытия соединения.
	// Это решает deadlock.
	select {
	case <-ctx.Done():
		log.Printf("Consumer: Context cancelled for tag '%s'. Shutting down consumer.", c.config.ConsumerTag)
		// Это штатное завершение. Мы получили сигнал, что пора выходить.
		// Внутренняя горутина тоже увидит ctx.Done() и завершится.
		// Мы возвращаем nil, потому что это не ошибка, а graceful shutdown.
		return nil

	case err := <-notifyClose:
		// Соединение было закрыто брокером или другим компонентом.
		// Это, как правило, ошибка, которую нужно обработать.
		log.Printf("Consumer: Connection closed for tag '%s'. Error: %v", c.config.ConsumerTag, err)
		return err // Возвращаем ошибку от RabbitMQ
	}
}

// Close закрывает соединение потребителя
func (c *Consumer) Close() error {
	log.Println("Consumer: Closing...")

	// Отменяем контекст, чтобы остановить цикл потребления
	// if c.cancelFunc != nil {
	// 	c.cancelFunc()
	// }

	// Ждем завершения всех горутин-обработчиков
	log.Println("Consumer: Waiting for message handlers to finish...")
	c.wg.Wait()
	log.Println("Consumer: All message handlers finished.")

	var firstErr error

	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			log.Printf("Consumer: Error closing channel: %v\n", err)
			firstErr = err
		}
		c.channel = nil
	}
	if c.connection != nil {
		if err := c.connection.Close(); err != nil {
			log.Printf("Consumer: Error closing connection: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		c.connection = nil
	}
	log.Println("Consumer: Closed.")
	return firstErr
}