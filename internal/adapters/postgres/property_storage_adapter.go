package postgres

import (
	"context"
	"fmt"
	"strings"

	"parser-project/internal/core/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStorageAdapter реализует PropertyStoragePort для PostgreSQL.
type PostgresStorageAdapter struct {
	pool *pgxpool.Pool
}

// NewPostgresStorageAdapter создает новый экземпляр адаптера.
func NewPostgresStorageAdapter(pool *pgxpool.Pool) (*PostgresStorageAdapter, error) {
	if pool == nil {
		return nil, fmt.Errorf("pgxpool.Pool cannot be nil")
	}
	return &PostgresStorageAdapter{
		pool: pool,
	}, nil
}

// Save сохраняет одну запись PropertyRecord в базу данных.
// Эта реализация будет простой, без проверки дубликатов.
// Логику проверки дубликатов и пакетной вставки мы добавим позже.
func (a *PostgresStorageAdapter) Save(ctx context.Context, record domain.PropertyRecord) error {
	// Собираем SQL-запрос. Мы перечисляем все колонки, которые хотим вставить.
	// Системные поля вроде id, created_at будут заполнены базой данных автоматически.
	// Мы также будем устанавливать updated_at в NOW() при каждой вставке/обновлении.
	
	// ВАЖНО: Мы используем ON CONFLICT DO UPDATE, также известный как "UPSERT".
	// Если запись с таким `source` уже существует, она будет обновлена.
	// Если нет - будет вставлена новая. Это решает проблему дубликатов по URL.
	// Для вашей задачи с проверкой по набору полей логика будет сложнее (см. ниже).
	// Пока что для простоты начнем с UPSERT по `source`.
	
	// Список колонок для вставки
	columns := []string{
		"updated_at", "is_duplicated", "partner", "source", "slug", "title", "description",
		"images", "preview_image", "address", "district", "coordinates", "contacts",
		"transaction_type", "estate", "advert_type", "advertiser_type", "status", "total_price",
		"rent_price", "deposit_price", "price_per_square_meter", "currency", "area_in_square_meters",
		"rooms_number_string", "rooms_num", "floor_number", "building_floors", "note", "features",
		"parsed_features", "marks", "published_at", "site_created_at", "site_updated_at",
	}

	// Создаем строку с плейсхолдерами ($1, $2, $3...)
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	// Собираем значения в правильном порядке
	values := []interface{}{
		record.UpdatedAt, record.IsDuplicated, record.Partner, record.Source, record.Slug, record.Title, record.Description,
		record.Images, record.PreviewImage, record.Address, record.District, record.Coordinates, record.Contacts,
		record.TransactionType, record.Estate, record.AdvertType, record.AdvertiserType, record.Status, record.TotalPrice,
		record.RentPrice, record.DepositPrice, record.PricePerSquareMeter, record.Currency, record.AreaInSquareMeters,
		record.RoomsNumString, record.RoomsNum, record.FloorNumber, record.BuildingFloors, record.Note, record.Features,
		record.ParsedFeatures, record.Marks, record.PublishedAt, record.SiteCreatedAt, record.SiteUpdatedAt,
	}

    // Собираем финальный SQL запрос
	sql := fmt.Sprintf(
		`INSERT INTO properties (%s) VALUES (%s)`,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
    
	// Выполняем запрос. Exec используется для запросов, которые не возвращают строк (INSERT, UPDATE, DELETE).
	_, err := a.pool.Exec(ctx, sql, values...)

	if err != nil {
		// Здесь можно и нужно добавить более детальную обработку ошибок,
		// например, проверку на specific PostgreSQL error codes.
		return fmt.Errorf("failed to insert property record into db (source: %s): %w", record.Source, err)
	}

	return nil
}