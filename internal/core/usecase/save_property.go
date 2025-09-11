package usecase

import (
	"context"
	"fmt"
	"log"
	"parser-project/internal/core/domain"
	"parser-project/internal/core/port"
)

// SavePropertyUseCase инкапсулирует логику сохранения PropertyRecord.
type SavePropertyUseCase struct {
	storage port.PropertyStoragePort
}

// NewSavePropertyUseCase создает новый экземпляр use case.
func NewSavePropertyUseCase(storage port.PropertyStoragePort) *SavePropertyUseCase {
	return &SavePropertyUseCase{
		storage: storage,
	}
}

// Execute выполняет основную логику: сохраняет запись, используя порт хранилища.
func (uc *SavePropertyUseCase) Execute(ctx context.Context, record domain.PropertyRecord) error {
	log.Printf("SavePropertyUseCase: Attempting to save record from source %s\n", record.Source)

	if err := uc.storage.Save(ctx, record); err != nil {
		return fmt.Errorf("failed to save property record from source %s: %w", record.Source, err)
	}

	log.Printf("SavePropertyUseCase: Successfully saved record from source %s\n", record.Source)
	return nil
}