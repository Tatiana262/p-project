package rabbitmq_producer

import (
	"context"
	"fmt"
	"log"
	"parser-project/pkg/rabbitmq/rabbitmq_common"
	amqp "github.com/rabbitmq/amqp091-go"
)

// PublisherConfig конфигурация для производителя
type PublisherConfig struct {
	rabbitmq_common.Config        
	ExchangeName          string // Имя обменника для публикации
	ExchangeType          string // Тип обменника (direct, fanout, topic, headers)
	DurableExchange       bool   // Долговечность обменника
	AutoDeleteExchange    bool   // Автоудаление обменника 
	InternalExchange      bool   // Внутренний ли обменник 
	ExchangeArgs          amqp.Table // Дополнительные аргументы для обменника (например, alternate-exchange)

	// Флаг, указывающий, нужно ли пытаться объявить обменник (если false, производитель будет полагаться на то, что обменник уже существует)
	DeclareExchangeIfMissing bool
}

// Publisher структура для управления производителем
type Publisher struct {
	config     PublisherConfig
	connection *amqp.Connection
	channel    *amqp.Channel
}

// NewPublisher создает нового производителя
func NewPublisher(cfg PublisherConfig) (*Publisher, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid base config: %w", err)
	}
	// Валидация специфичная для PublisherConfig
	if cfg.DeclareExchangeIfMissing && cfg.ExchangeName == "" && cfg.ExchangeType != "" { 
		return nil, fmt.Errorf("producer: exchange name is required if ExchangeType is specified and DeclareExchangeIfMissing is true")
	}
	if cfg.DeclareExchangeIfMissing && cfg.ExchangeType == "" && cfg.ExchangeName != "" {
		return nil, fmt.Errorf("producer: exchange type is required if ExchangeName is specified and DeclareExchangeIfMissing is true")
	}


	p := &Publisher{
		config: cfg,
	}

	conn, err := amqp.Dial(p.config.URL)
	if err != nil {
		return nil, fmt.Errorf("producer: failed to dial RabbitMQ: %w", err)
	}
	p.connection = conn

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("producer: failed to open a channel: %w", err)
	}
	p.channel = ch

	// Объявляем обменник, если это указано в конфигурации
	if p.config.DeclareExchangeIfMissing {
		log.Printf("Producer: Declaring exchange '%s' (type: %s, durable: %v, autoDelete: %v, internal: %v)\n",
			p.config.ExchangeName, p.config.ExchangeType, p.config.DurableExchange, p.config.AutoDeleteExchange, p.config.InternalExchange)
		err = ch.ExchangeDeclare(
			p.config.ExchangeName,
			p.config.ExchangeType,
			p.config.DurableExchange,
			p.config.AutoDeleteExchange,
			p.config.InternalExchange,
			false, // no-wait
			p.config.ExchangeArgs,
		)
		if err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, fmt.Errorf("producer: failed to declare exchange '%s': %w", p.config.ExchangeName, err)
		}
	} else if p.config.ExchangeName != "" {
		log.Printf("Producer: Assuming exchange '%s' already exists (DeclareExchangeIfMissing is false or type not specified).\n", p.config.ExchangeName)
	}


	log.Println("Producer: Successfully connected and channel opened.")
	return p, nil
}

// Publish публикует сообщение
func (p *Publisher) Publish(ctx context.Context, routingKey string, msg amqp.Publishing) error {
	if p.channel == nil || p.connection == nil || p.connection.IsClosed() {
		return fmt.Errorf("producer: not connected or channel/connection is closed")
	}

	err := p.channel.PublishWithContext(
		ctx,
		p.config.ExchangeName, // имя обменника из конфигурации (пустая строка для default exchange)
		routingKey,
		false, // mandatory
		false, // immediate
		msg,
	)
	if err != nil {
		return fmt.Errorf("producer: failed to publish message: %w", err)
	}
	return nil
}

// Close закрывает соединение производителя
func (p *Publisher) Close() error {
	log.Println("Producer: Closing...")
	var firstErr error

	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			log.Printf("Producer: Error closing channel: %v\n", err)
			firstErr = err
		}
		p.channel = nil
	}
	if p.connection != nil {
		if err := p.connection.Close(); err != nil {
			log.Printf("Producer: Error closing connection: %v\n", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		p.connection = nil
	}
	log.Println("Producer: Closed.")
	return firstErr
}