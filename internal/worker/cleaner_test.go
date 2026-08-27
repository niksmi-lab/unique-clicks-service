package worker

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type cleanupStub struct {
	called chan int
}

func (s *cleanupStub) DeleteExpired(_ context.Context, retentionDays int) (int64, error) {
	s.called <- retentionDays
	return 3, nil
}

func TestCleanerRunsAtStartupAndStops(t *testing.T) {
	service := &cleanupStub{called: make(chan int, 1)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cleaner := NewCleaner(service, logger, time.Hour, time.Second, 30)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cleaner.Run(ctx)
		close(done)
	}()

	select {
	case retention := <-service.called:
		if retention != 30 {
			t.Fatalf("retentionDays = %d, want 30", retention)
		}
	case <-time.After(time.Second):
		t.Fatal("startup cleanup did not run")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleaner did not stop after context cancellation")
	}
}
