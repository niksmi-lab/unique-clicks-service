package worker

import (
	"context"
	"log/slog"
	"time"
)

type CleanupService interface {
	DeleteExpired(ctx context.Context, retentionDays int) (int64, error)
}

type Observer interface {
	ObserveCleanup(result string, deletedRows int64, duration time.Duration)
}

type noopObserver struct{}

func (noopObserver) ObserveCleanup(string, int64, time.Duration) {}

type Cleaner struct {
	service       CleanupService
	logger        *slog.Logger
	observer      Observer
	interval      time.Duration
	timeout       time.Duration
	retentionDays int
}

func NewCleaner(service CleanupService, logger *slog.Logger, interval, timeout time.Duration, retentionDays int, observers ...Observer) *Cleaner {
	observer := Observer(noopObserver{})
	if len(observers) > 0 && observers[0] != nil {
		observer = observers[0]
	}
	return &Cleaner{
		service:       service,
		logger:        logger,
		observer:      observer,
		interval:      interval,
		timeout:       timeout,
		retentionDays: retentionDays,
	}
}

// Run performs cleanup at startup and then periodically until ctx is cancelled.
func (c *Cleaner) Run(ctx context.Context) {
	c.runOnce(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runOnce(ctx)
		}
	}
}

func (c *Cleaner) runOnce(parent context.Context) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	deleted, err := c.service.DeleteExpired(ctx, c.retentionDays)
	if err != nil {
		c.observer.ObserveCleanup("error", 0, time.Since(started))
		if parent.Err() == nil {
			c.logger.Error("expired clicks cleanup failed", "error", err)
		}
		return
	}
	c.observer.ObserveCleanup("success", deleted, time.Since(started))
	c.logger.Info("expired clicks cleanup completed", "deleted_rows", deleted)
}
