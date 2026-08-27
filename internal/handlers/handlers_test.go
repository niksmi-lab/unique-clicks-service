package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type analyticsStub struct {
	trackUserID   int64
	trackAuthorID int64
	trackErr      error
	metrics       map[int64]int64
	metricsErr    error
	metricIDs     []int64
}

func (s *analyticsStub) TrackClick(_ context.Context, userID, authorID int64) error {
	s.trackUserID = userID
	s.trackAuthorID = authorID
	return s.trackErr
}

func (s *analyticsStub) GetYesterdayMetrics(_ context.Context, ids []int64) (map[int64]int64, error) {
	s.metricIDs = ids
	return s.metrics, s.metricsErr
}

func testHandler(service Analytics) *AnalyticsHandler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAnalyticsHandler(service, logger, time.Second, 3)
}

func jsonRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestHandleClickAccepted(t *testing.T) {
	service := &analyticsStub{}
	handler := testHandler(service)
	response := httptest.NewRecorder()

	handler.HandleClick(response, jsonRequest(http.MethodPost, "/v1/clicks", `{"user_id":101,"author_id":42}`))

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if service.trackUserID != 101 || service.trackAuthorID != 42 {
		t.Fatalf("service args = (%d, %d)", service.trackUserID, service.trackAuthorID)
	}
}

func TestHandleClickRejectsUnknownField(t *testing.T) {
	service := &analyticsStub{}
	handler := testHandler(service)
	response := httptest.NewRecorder()

	handler.HandleClick(response, jsonRequest(http.MethodPost, "/v1/clicks", `{"user_id":1,"author_id":2,"extra":true}`))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "invalid_request") {
		t.Fatalf("body = %s, want invalid_request error", response.Body.String())
	}
}

func TestHandleClickDoesNotLeakStorageError(t *testing.T) {
	service := &analyticsStub{trackErr: errors.New("password=secret")}
	handler := testHandler(service)
	response := httptest.NewRecorder()

	handler.HandleClick(response, jsonRequest(http.MethodPost, "/v1/clicks", `{"user_id":1,"author_id":2}`))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("response leaks internal error: %s", response.Body.String())
	}
}

func TestHandleMetrics(t *testing.T) {
	service := &analyticsStub{metrics: map[int64]int64{42: 5, 43: 0}}
	handler := testHandler(service)
	response := httptest.NewRecorder()

	handler.HandleMetrics(response, jsonRequest(http.MethodPost, "/v1/metrics/yesterday", `{"author_ids":[42,43]}`))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Body.String(); !strings.Contains(got, `"42":5`) || !strings.Contains(got, `"43":0`) {
		t.Fatalf("body = %s, want both metrics", got)
	}
}

func TestHandleMetricsLimitsBatch(t *testing.T) {
	service := &analyticsStub{}
	handler := testHandler(service)
	response := httptest.NewRecorder()

	handler.HandleMetrics(response, jsonRequest(http.MethodPost, "/v1/metrics/yesterday", `{"author_ids":[1,2,3,4]}`))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "too_many_authors") {
		t.Fatalf("body = %s, want too_many_authors error", response.Body.String())
	}
}

func TestHandleClickRequiresJSONContentType(t *testing.T) {
	handler := testHandler(&analyticsStub{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/clicks", strings.NewReader("{}"))

	handler.HandleClick(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

type pingerStub struct {
	err error
}

func (p pingerStub) Ping(context.Context) error {
	return p.err
}

func TestReadiness(t *testing.T) {
	handler := NewHealthHandler(pingerStub{}, time.Second)
	response := httptest.NewRecorder()

	handler.Ready(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestReadinessUnavailable(t *testing.T) {
	handler := NewHealthHandler(pingerStub{err: errors.New("down")}, time.Second)
	response := httptest.NewRecorder()

	handler.Ready(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}
