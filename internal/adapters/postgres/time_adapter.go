package postgres

import (
	"context"
	"errors"
	"log"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresLastRunRepository реализует LastRunRepositoryPort для PostgreSQL.
type PostgresLastRunRepository struct {
	dbPool *pgxpool.Pool
}

// NewPostgresLastRunRepository создает новый экземпляр PostgresLastRunRepository.
func NewPostgresLastRunRepository(dbPool *pgxpool.Pool) (*PostgresLastRunRepository, error) {
	if dbPool == nil {
		return nil, fmt.Errorf("postgres last run repository: dbPool cannot be nil")
	}
	return &PostgresLastRunRepository{dbPool: dbPool}, nil
}

// GetLastRunTimestamp извлекает время последнего запуска для указанного парсера.
func (r *PostgresLastRunRepository) GetLastRunTimestamp(ctx context.Context, parserName string) (time.Time, error) {
	var lastRun time.Time
	query := `SELECT last_run_timestamp FROM parser_last_runs WHERE parser_name = $1`

	log.Printf("PostgresLastRunRepo: Getting last run timestamp for parser '%s'\n", parserName)

	// Выполняем запрос и сканируем результат в переменную lastRun
	err := r.dbPool.QueryRow(ctx, query, parserName).Scan(&lastRun)
	if err != nil {
		// ПРАВИЛЬНАЯ проверка на "не найдено" для pgx/v5
		if errors.Is(err, pgx.ErrNoRows) {
			log.Printf("PostgresLastRunRepo: No last run timestamp found for parser '%s'.", parserName)
			// Возвращаем нулевое время и специальную ошибку, чтобы UseCase мог ее обработать.
			// Либо можно просто вернуть time.Time{} и nil, если отсутствие записи не считается ошибкой.
			// Но лучше вернуть ошибку, это более явно.
			return time.Time{}, nil
		}
		
		// Если это любая другая ошибка (проблемы с соединением и т.д.)
		log.Printf("PostgresLastRunRepo: Error getting last run timestamp for parser '%s': %v\n", parserName, err)
		return time.Time{}, fmt.Errorf("error querying last run for parser '%s': %w", parserName, err)
	}

	log.Printf("PostgresLastRunRepo: Found last run timestamp for parser '%s': %s\n", parserName, lastRun.Format(time.RFC3339))
	return lastRun, nil
}

// SetLastRunTimestamp устанавливает или обновляет время последнего запуска для указанного парсера.
func (r *PostgresLastRunRepository) SetLastRunTimestamp(ctx context.Context, parserName string, t time.Time) error {
	// Используем ON CONFLICT (UPSERT) — это самый эффективный и атомарный способ.
	query := `
        INSERT INTO parser_last_runs (parser_name, last_run_timestamp)
        VALUES ($1, $2)
        ON CONFLICT (parser_name) DO UPDATE SET last_run_timestamp = EXCLUDED.last_run_timestamp
    `
	log.Printf("PostgresLastRunRepo: Setting last run timestamp for parser '%s' to %s\n", parserName, t.Format(time.RFC3339))

	// Выполняем запрос
	_, err := r.dbPool.Exec(ctx, query, parserName, t)
	if err != nil {
		log.Printf("PostgresLastRunRepo: Error setting last run timestamp for parser '%s': %v\n", parserName, err)
		return fmt.Errorf("error setting last run for parser '%s': %w", parserName, err)
	}

	log.Printf("PostgresLastRunRepo: Successfully set last run timestamp for parser '%s'\n", parserName)
	return nil
}


// CREATE TABLE IF NOT EXISTS parser_last_runs (
//     parser_name VARCHAR(255) PRIMARY KEY,
//     last_run_timestamp TIMESTAMPTZ NOT NULL
// );

// -- Можно добавить индекс для ускорения поиска
// CREATE INDEX IF NOT EXISTS idx_parser_last_runs_parser_name ON parser_last_runs(parser_name);