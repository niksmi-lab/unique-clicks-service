package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/niksmi-lab/unique-clicks-service/internal/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Observer interface {
	ObserveStorageOperation(operation, result string, duration time.Duration)
}

type noopObserver struct{}

func (noopObserver) ObserveStorageOperation(string, string, time.Duration) {}

type Storage struct {
	pool     *pgxpool.Pool
	observer Observer
}

func New(pool *pgxpool.Pool, observers ...Observer) *Storage {
	observer := Observer(noopObserver{})
	if len(observers) > 0 && observers[0] != nil {
		observer = observers[0]
	}
	return &Storage{pool: pool, observer: observer}
}

// RecordClick is idempotent: the primary key prevents a user from increasing
// the same author's counter more than once per UTC day.
func (s *Storage) RecordClick(ctx context.Context, click models.Click) (bool, error) {
	started := time.Now()
	result := "success"
	defer func() {
		s.observer.ObserveStorageOperation("record_click", result, time.Since(started))
	}()

	const query = `
		INSERT INTO clicks (author_id, user_id, click_date)
		VALUES ($1, $2, $3)
		ON CONFLICT (click_date, author_id, user_id) DO NOTHING`

	command, err := s.pool.Exec(ctx, query, click.AuthorID, click.UserID, click.Date)
	if err != nil {
		result = "error"
		return false, fmt.Errorf("insert click: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (s *Storage) UniqueClicksByAuthors(ctx context.Context, date time.Time, authorIDs []int64) (map[int64]int64, error) {
	started := time.Now()
	result := "success"
	defer func() {
		s.observer.ObserveStorageOperation("read_metrics", result, time.Since(started))
	}()

	const query = `
		SELECT author_id, COUNT(*)
		FROM clicks
		WHERE click_date = $1 AND author_id = ANY($2::BIGINT[])
		GROUP BY author_id`

	rows, err := s.pool.Query(ctx, query, date, authorIDs)
	if err != nil {
		result = "error"
		return nil, fmt.Errorf("query metrics: %w", err)
	}
	defer rows.Close()

	metrics := make(map[int64]int64, len(authorIDs))
	for rows.Next() {
		var authorID, count int64
		if err := rows.Scan(&authorID, &count); err != nil {
			result = "error"
			return nil, fmt.Errorf("scan metrics row: %w", err)
		}
		metrics[authorID] = count
	}
	if err := rows.Err(); err != nil {
		result = "error"
		return nil, fmt.Errorf("iterate metrics rows: %w", err)
	}
	return metrics, nil
}

func (s *Storage) DeleteClicksBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	started := time.Now()
	result := "success"
	defer func() {
		s.observer.ObserveStorageOperation("cleanup", result, time.Since(started))
	}()

	command, err := s.pool.Exec(ctx, `DELETE FROM clicks WHERE click_date < $1`, cutoff)
	if err != nil {
		result = "error"
		return 0, fmt.Errorf("delete clicks: %w", err)
	}
	return command.RowsAffected(), nil
}

func (s *Storage) Ping(ctx context.Context) error {
	started := time.Now()
	result := "success"
	defer func() {
		s.observer.ObserveStorageOperation("ping", result, time.Since(started))
	}()

	if err := s.pool.Ping(ctx); err != nil {
		result = "error"
		return err
	}
	return nil
}
