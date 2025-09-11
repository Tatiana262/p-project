package port

import (
	"context"
	"parser-project/internal/core/domain"
)

// PropertyLinkQueuePort определяет контракт для отправки ссылок в очередь.
type ProcessedPropertyQueuePort interface {
	Enqueue(ctx context.Context, link domain.PropertyRecord) error
}