package httpmw

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiddlewareAddsOperationalMetadata(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	endpoint := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})

	handler := RequestID(AccessLog(logger, Recover(logger, SecurityHeaders(endpoint))))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/resource", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	if id := response.Header().Get("X-Request-ID"); len(id) != 24 {
		t.Fatalf("X-Request-ID = %q, want 24 hex characters", id)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
	if got := logs.String(); !strings.Contains(got, `"status":201`) || !strings.Contains(got, `"path":"/resource"`) {
		t.Fatalf("access log = %s", got)
	}
}

func TestRecoverConvertsPanicToInternalError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	endpoint := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	handler := RequestID(Recover(logger, endpoint))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(response.Body.String(), "internal_error") {
		t.Fatalf("body = %s, want internal_error", response.Body.String())
	}
}

type metricsObserverStub struct {
	inFlight int
	method   string
	route    string
	status   int
	duration time.Duration
}

func (m *metricsObserverStub) IncHTTPInFlight() {
	m.inFlight++
}

func (m *metricsObserverStub) DecHTTPInFlight() {
	m.inFlight--
}

func (m *metricsObserverStub) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	m.method = method
	m.route = route
	m.status = status
	m.duration = duration
}

func TestMetricsUsesRoutePatternAndStatus(t *testing.T) {
	observer := &metricsObserverStub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /resource/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := Metrics(observer, mux)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/resource/123", nil))

	if observer.inFlight != 0 {
		t.Fatalf("inFlight = %d, want 0", observer.inFlight)
	}
	if observer.method != http.MethodGet || observer.route != "GET /resource/{id}" || observer.status != http.StatusNoContent {
		t.Fatalf("labels = (%q, %q, %d)", observer.method, observer.route, observer.status)
	}
	if observer.duration <= 0 {
		t.Fatalf("duration = %v, want positive", observer.duration)
	}
}
