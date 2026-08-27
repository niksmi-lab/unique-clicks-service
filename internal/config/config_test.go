package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadOverrides(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("REQUEST_TIMEOUT", "2s")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")
	t.Setenv("CLEANUP_INTERVAL", "10m")
	t.Setenv("RETENTION_DAYS", "7")
	t.Setenv("DB_MAX_CONNECTIONS", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment != "production" || cfg.ServerAddr != ":9090" || cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("unexpected string config: %+v", cfg)
	}
	if cfg.RequestTimeout != 2*time.Second || cfg.ShutdownTimeout != 3*time.Second || cfg.CleanupInterval != 10*time.Minute {
		t.Fatalf("unexpected duration config: %+v", cfg)
	}
	if cfg.RetentionDays != 7 || cfg.MaxDBConnections != 20 {
		t.Fatalf("unexpected numeric config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValue(t *testing.T) {
	t.Setenv("RETENTION_DAYS", "zero")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "RETENTION_DAYS") {
		t.Fatalf("error = %v, want RETENTION_DAYS validation error", err)
	}
}
