package port

import (
	"context"
	"parser-project/internal/core/domain"
)

// PropertyStoragePort определяет контракт для сохранения
// обработанного объекта недвижимости в постоянное хранилище.
type PropertyStoragePort interface {
	Save(ctx context.Context, record domain.PropertyRecord) error
}