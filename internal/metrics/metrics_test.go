package metrics

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMetricsAreExposed(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://clicks:clicks@127.0.0.1:1/clicks?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("NewWithConfig() error = %v", err)
	}
	defer pool.Close()

	telemetry := New(pool, "test-version")
	telemetry.ObserveHTTPRequest("POST", "POST /v1/clicks", 202, 25*time.Millisecond)
	telemetry.ObserveClick("recorded")
	telemetry.ObserveClick("duplicate")
	telemetry.ObserveMetricsQuery("success")
	telemetry.ObserveStorageOperation("record_click", "success", 5*time.Millisecond)
	telemetry.ObserveCleanup("success", 3, 10*time.Millisecond)

	response := httptest.NewRecorder()
	telemetry.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))

	if response.Code != 200 {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	expected := []string{
		`unique_clicks_build_info{go_version=`,
		`unique_clicks_http_requests_total{method="POST",route="POST /v1/clicks",status="202"} 1`,
		`unique_clicks_clicks_total{result="recorded"} 1`,
		`unique_clicks_clicks_total{result="duplicate"} 1`,
		`unique_clicks_metrics_queries_total{result="success"} 1`,
		`unique_clicks_storage_operations_total{operation="record_click",result="success"} 1`,
		`unique_clicks_cleanup_deleted_rows_total 3`,
		`go_goroutines`,
		`process_cpu_seconds_total`,
	}
	for _, value := range expected {
		if !strings.Contains(body, value) {
			t.Errorf("metrics output does not contain %q", value)
		}
	}
}
