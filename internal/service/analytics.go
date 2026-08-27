package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/niksmi-lab/unique-clicks-service/internal/models"
)

var (
	ErrInvalidUserID   = errors.New("user id must be positive")
	ErrInvalidAuthorID = errors.New("author id must be positive")
	ErrNoAuthorIDs     = errors.New("at least one author id is required")
)

// Storage describes the persistence operations required by the business layer.
type Storage interface {
	RecordClick(ctx context.Context, click models.Click) (bool, error)
	UniqueClicksByAuthors(ctx context.Context, date time.Time, authorIDs []int64) (map[int64]int64, error)
	DeleteClicksBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

type Observer interface {
	ObserveClick(result string)
	ObserveMetricsQuery(result string)
}

type noopObserver struct{}

func (noopObserver) ObserveClick(string)        {}
func (noopObserver) ObserveMetricsQuery(string) {}

type Clock func() time.Time

type AnalyticsService struct {
	store    Storage
	now      Clock
	observer Observer
}

func NewAnalyticsService(store Storage, observers ...Observer) *AnalyticsService {
	return newAnalyticsService(store, time.Now, observers...)
}

// NewAnalyticsServiceWithClock is primarily useful for deterministic tests.
func NewAnalyticsServiceWithClock(store Storage, now Clock, observers ...Observer) *AnalyticsService {
	return newAnalyticsService(store, now, observers...)
}

func (s *AnalyticsService) TrackClick(ctx context.Context, userID, authorID int64) error {
	if userID <= 0 {
		s.observer.ObserveClick("invalid")
		return ErrInvalidUserID
	}
	if authorID <= 0 {
		s.observer.ObserveClick("invalid")
		return ErrInvalidAuthorID
	}

	recorded, err := s.store.RecordClick(ctx, models.Click{
		UserID:   userID,
		AuthorID: authorID,
		Date:     midnightUTC(s.now().UTC()),
	})
	if err != nil {
		s.observer.ObserveClick("error")
		return fmt.Errorf("record click: %w", err)
	}
	if recorded {
		s.observer.ObserveClick("recorded")
	} else {
		s.observer.ObserveClick("duplicate")
	}
	return nil
}

func (s *AnalyticsService) GetYesterdayMetrics(ctx context.Context, authorIDs []int64) (map[int64]int64, error) {
	authorIDs, err := uniquePositiveIDs(authorIDs)
	if err != nil {
		s.observer.ObserveMetricsQuery("invalid")
		return nil, err
	}

	now := s.now().UTC()
	yesterday := midnightUTC(now).AddDate(0, 0, -1)
	metrics, err := s.store.UniqueClicksByAuthors(ctx, yesterday, authorIDs)
	if err != nil {
		s.observer.ObserveMetricsQuery("error")
		return nil, fmt.Errorf("get unique clicks: %w", err)
	}
	if metrics == nil {
		metrics = make(map[int64]int64, len(authorIDs))
	}

	// Requested authors without clicks are valid metrics with a zero value.
	for _, id := range authorIDs {
		if _, ok := metrics[id]; !ok {
			metrics[id] = 0
		}
	}
	s.observer.ObserveMetricsQuery("success")
	return metrics, nil
}

// DeleteExpired removes complete UTC days older than retentionDays.
func (s *AnalyticsService) DeleteExpired(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays < 1 {
		return 0, errors.New("retention days must be positive")
	}
	cutoff := midnightUTC(s.now().UTC()).AddDate(0, 0, -retentionDays)
	deleted, err := s.store.DeleteClicksBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired clicks: %w", err)
	}
	return deleted, nil
}

func uniquePositiveIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, ErrNoAuthorIDs
	}

	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrInvalidAuthorID
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	return result, nil
}

func newAnalyticsService(store Storage, now Clock, observers ...Observer) *AnalyticsService {
	observer := Observer(noopObserver{})
	if len(observers) > 0 && observers[0] != nil {
		observer = observers[0]
	}
	return &AnalyticsService{store: store, now: now, observer: observer}
}

func midnightUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
