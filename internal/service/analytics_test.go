package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/niksmi-lab/unique-clicks-service/internal/models"
)

type storageStub struct {
	recorded       models.Click
	recordCreated  bool
	recordErr      error
	metrics        map[int64]int64
	metricsErr     error
	metricsDate    time.Time
	metricsAuthors []int64
	deleted        int64
	deleteErr      error
	deleteCutoff   time.Time
}

func (s *storageStub) RecordClick(_ context.Context, click models.Click) (bool, error) {
	s.recorded = click
	return s.recordCreated, s.recordErr
}

func (s *storageStub) UniqueClicksByAuthors(_ context.Context, date time.Time, ids []int64) (map[int64]int64, error) {
	s.metricsDate = date
	s.metricsAuthors = ids
	return s.metrics, s.metricsErr
}

func (s *storageStub) DeleteClicksBefore(_ context.Context, cutoff time.Time) (int64, error) {
	s.deleteCutoff = cutoff
	return s.deleted, s.deleteErr
}

type observerStub struct {
	clickResults  []string
	metricResults []string
}

func (o *observerStub) ObserveClick(result string) {
	o.clickResults = append(o.clickResults, result)
}

func (o *observerStub) ObserveMetricsQuery(result string) {
	o.metricResults = append(o.metricResults, result)
}

func TestTrackClick(t *testing.T) {
	now := time.Date(2026, 8, 27, 3, 30, 0, 0, time.FixedZone("MSK", 3*60*60))
	store := &storageStub{recordCreated: true}
	observer := &observerStub{}
	svc := NewAnalyticsServiceWithClock(store, func() time.Time { return now }, observer)

	if err := svc.TrackClick(context.Background(), 101, 42); err != nil {
		t.Fatalf("TrackClick() error = %v", err)
	}

	if store.recorded.UserID != 101 || store.recorded.AuthorID != 42 {
		t.Fatalf("recorded click = %+v", store.recorded)
	}
	wantDate := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	if !store.recorded.Date.Equal(wantDate) {
		t.Fatalf("Date = %v, want %v", store.recorded.Date, wantDate)
	}
	if !reflect.DeepEqual(observer.clickResults, []string{"recorded"}) {
		t.Fatalf("click metric results = %v", observer.clickResults)
	}
}

func TestTrackClickDuplicateMetric(t *testing.T) {
	observer := &observerStub{}
	svc := NewAnalyticsServiceWithClock(&storageStub{}, time.Now, observer)

	if err := svc.TrackClick(context.Background(), 1, 2); err != nil {
		t.Fatalf("TrackClick() error = %v", err)
	}
	if !reflect.DeepEqual(observer.clickResults, []string{"duplicate"}) {
		t.Fatalf("click metric results = %v", observer.clickResults)
	}
}

func TestTrackClickValidationAndStorageError(t *testing.T) {
	store := &storageStub{}
	observer := &observerStub{}
	svc := NewAnalyticsServiceWithClock(store, time.Now, observer)

	if err := svc.TrackClick(context.Background(), 0, 1); !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("error = %v, want ErrInvalidUserID", err)
	}
	if err := svc.TrackClick(context.Background(), 1, -1); !errors.Is(err, ErrInvalidAuthorID) {
		t.Fatalf("error = %v, want ErrInvalidAuthorID", err)
	}

	store.recordErr = errors.New("database unavailable")
	if err := svc.TrackClick(context.Background(), 1, 2); err == nil || !errors.Is(err, store.recordErr) {
		t.Fatalf("error = %v, want wrapped storage error", err)
	}
	if want := []string{"invalid", "invalid", "error"}; !reflect.DeepEqual(observer.clickResults, want) {
		t.Fatalf("click metric results = %v, want %v", observer.clickResults, want)
	}
}

func TestGetYesterdayMetrics(t *testing.T) {
	now := time.Date(2026, 8, 27, 0, 30, 0, 0, time.UTC)
	store := &storageStub{metrics: map[int64]int64{42: 7}}
	observer := &observerStub{}
	svc := NewAnalyticsServiceWithClock(store, func() time.Time { return now }, observer)

	got, err := svc.GetYesterdayMetrics(context.Background(), []int64{42, 42, 43})
	if err != nil {
		t.Fatalf("GetYesterdayMetrics() error = %v", err)
	}

	wantDate := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	if !store.metricsDate.Equal(wantDate) {
		t.Fatalf("date = %v, want %v", store.metricsDate, wantDate)
	}
	if !reflect.DeepEqual(store.metricsAuthors, []int64{42, 43}) {
		t.Fatalf("authors = %v, want [42 43]", store.metricsAuthors)
	}
	if want := map[int64]int64{42: 7, 43: 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(observer.metricResults, []string{"success"}) {
		t.Fatalf("query metric results = %v", observer.metricResults)
	}
}

func TestDeleteExpired(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 10, 0, 0, time.UTC)
	store := &storageStub{deleted: 12}
	svc := NewAnalyticsServiceWithClock(store, func() time.Time { return now })

	deleted, err := svc.DeleteExpired(context.Background(), 30)
	if err != nil {
		t.Fatalf("DeleteExpired() error = %v", err)
	}
	if deleted != 12 {
		t.Fatalf("deleted = %d, want 12", deleted)
	}

	wantCutoff := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	if !store.deleteCutoff.Equal(wantCutoff) {
		t.Fatalf("cutoff = %v, want %v", store.deleteCutoff, wantCutoff)
	}
}
