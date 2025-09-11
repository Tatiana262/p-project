package usecase

import (
	"context"
	"fmt"
	"log"
	"parser-project/internal/core/domain"
	"parser-project/internal/core/port"
)

// ProcessLinkUseCase инкапсулирует логику обработки одной ссылки:
// парсинг деталей и отправка результата в следующую очередь.
type ProcessLinkUseCase struct {
	detailsFetcher port.KufarFetcherPort
	resultQueue    port.ProcessedPropertyQueuePort
}

// NewProcessLinkUseCase создает новый экземпляр use case.
func NewProcessLinkUseCase(
	fetcher port.KufarFetcherPort,
	queue port.ProcessedPropertyQueuePort,
) *ProcessLinkUseCase {
	return &ProcessLinkUseCase{
		detailsFetcher: fetcher,
		resultQueue:    queue,
	}
}

// Execute выполняет основную логику use case.
func (uc *ProcessLinkUseCase) Execute(ctx context.Context, linkToParse domain.PropertyLink) error {
	log.Printf("ProcessLinkUseCase: Processing URL: %s\n", linkToParse.URL)

	// 1. Используем порт для парсинга деталей
	propertyRecord, fetchErr := uc.detailsFetcher.FetchAdDetails(ctx, linkToParse.URL)
	if fetchErr != nil {
		// Ошибка возвращается наверх, чтобы обработчик RabbitMQ мог решить, что делать (requeue/nack)
		return fmt.Errorf("failed to fetch/parse details for %s: %w", linkToParse.URL, fetchErr)
	}

	log.Printf("ProcessLinkUseCase: Successfully parsed details for URL %s. Title: %s\n", linkToParse.URL, propertyRecord.Title)

	// 2. Используем порт для отправки результата в очередь
	err := uc.resultQueue.Enqueue(ctx, *propertyRecord)
	if err != nil {
		// Если не удалось отправить, это критично. Возвращаем ошибку.
		return fmt.Errorf("CRITICAL: failed to enqueue processed data for URL %s: %w", linkToParse.URL, err)
	}

	log.Printf("ProcessLinkUseCase: Successfully enqueued processed data for '%s'.\n", linkToParse.URL)
	return nil
}