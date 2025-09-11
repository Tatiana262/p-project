package rabbitmq_common

import (
	"fmt"
)

// Config общая конфигурация для подключения к RabbitMQ
type Config struct {
	URL            string        // AMQP URL, например "amqp://guest:guest@localhost:5672/"
}

// Validate проверяет базовую конфигурацию
func (c *Config) Validate() error {
	if c.URL == "" {
		return fmt.Errorf("RabbitMQ URL configuration is required")
	}
	return nil
}

// FailOnError общая функция для обработки критических ошибок
func FailOnError(err error, msg string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %s", msg, err))
	}
}