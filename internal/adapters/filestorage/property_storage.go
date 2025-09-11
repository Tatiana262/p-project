package filestorage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"parser-project/internal/core/domain"
	"sync"
)

// PropertyFileStorageAdapter реализует PropertyStoragePort для сохранения в файл
type PropertyFileStorageAdapter struct {
	filename string
	mu       sync.Mutex // Для безопасной записи в файл из нескольких горутин
}

// NewPropertyFileStorageAdapter создает новый адаптер
func NewPropertyFileStorageAdapter(filename string) (*PropertyFileStorageAdapter, error) {
	if filename == "" {
		return nil, fmt.Errorf("filename cannot be empty")
	}
	return &PropertyFileStorageAdapter{
		filename: filename,
	}, nil
}

// Save сохраняет PropertyRecord в файл в форматированном виде
func (a *PropertyFileStorageAdapter) Save(_ context.Context, record domain.PropertyRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Сериализуем с отступами для читаемости
	prettyJSON, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to format record to pretty JSON for URL %s: %w", record.Source, err)
	}

	// Открываем файл для дозаписи
	file, err := os.OpenFile(a.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open output file '%s': %w", a.filename, err)
	}
	defer file.Close()

	// Собираем полный блок для записи
	separator := "\n\n"
	dataToWrite := append(prettyJSON, []byte(separator)...)

	if _, err := file.Write(dataToWrite); err != nil {
		return fmt.Errorf("failed to write to output file '%s': %w", a.filename, err)
	}

	log.Printf("FileStorageAdapter: Successfully saved record for URL %s to %s\n", record.Source, a.filename)
	return nil
}