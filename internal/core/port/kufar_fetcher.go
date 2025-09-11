package port

import (
	"context"
	"parser-project/internal/core/domain"
	"time"
)

// KufarFetcherPort объединяет все операции, которые можно выполнить
// с источником данных Kufar.
type KufarFetcherPort interface {
	// FetchLinks извлекает ссылки, соответствующие критериям.
	FetchLinks(ctx context.Context, criteria domain.Criteria, since time.Time) (links []domain.PropertyLink, nextCursor string, err error)
	
	// FetchAdDetails извлекает полную информацию об объекте недвижимости по его URL.
	FetchAdDetails(ctx context.Context, adURL string) (*domain.PropertyRecord, error)
}