package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Environment      string
	ServerAddr       string
	DatabaseURL      string
	RequestTimeout   time.Duration
	ShutdownTimeout  time.Duration
	CleanupInterval  time.Duration
	RetentionDays    int
	MaxDBConnections int32
}

func Load() (Config, error) {
	cfg := Config{
		Environment:      env("APP_ENV", "development"),
		ServerAddr:       env("SERVER_ADDR", ":8080"),
		DatabaseURL:      env("DATABASE_URL", "postgres://clicks:clicks@localhost:5433/clicks?sslmode=disable"),
		RequestTimeout:   5 * time.Second,
		ShutdownTimeout:  10 * time.Second,
		CleanupInterval:  time.Hour,
		RetentionDays:    30,
		MaxDBConnections: 10,
	}

	var err error
	if cfg.RequestTimeout, err = duration("REQUEST_TIMEOUT", cfg.RequestTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = duration("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.CleanupInterval, err = duration("CLEANUP_INTERVAL", cfg.CleanupInterval); err != nil {
		return Config{}, err
	}
	if cfg.RetentionDays, err = positiveInt("RETENTION_DAYS", cfg.RetentionDays); err != nil {
		return Config{}, err
	}
	connections, err := positiveInt("DB_MAX_CONNECTIONS", int(cfg.MaxDBConnections))
	if err != nil {
		return Config{}, err
	}
	cfg.MaxDBConnections = int32(connections)
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func positiveInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}
