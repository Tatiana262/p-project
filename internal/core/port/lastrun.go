package port

import (
	"context"
	"time"
)

// LastRunRepositoryPort определяет контракт для хранения и получения
// времени последнего успешного запуска парсера.
type LastRunRepositoryPort interface {
	GetLastRunTimestamp(ctx context.Context, parserName string) (time.Time, error)
	SetLastRunTimestamp(ctx context.Context, parserName string, t time.Time) error
}