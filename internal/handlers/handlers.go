package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/niksmi-lab/unique-clicks-service/internal/service"
)

const maxRequestBody = 1 << 20 // 1 MiB

type Analytics interface {
	TrackClick(ctx context.Context, userID, authorID int64) error
	GetYesterdayMetrics(ctx context.Context, authorIDs []int64) (map[int64]int64, error)
}

type AnalyticsHandler struct {
	service    Analytics
	logger     *slog.Logger
	timeout    time.Duration
	maxAuthors int
}

func NewAnalyticsHandler(s Analytics, logger *slog.Logger, timeout time.Duration, maxAuthors int) *AnalyticsHandler {
	return &AnalyticsHandler{service: s, logger: logger, timeout: timeout, maxAuthors: maxAuthors}
}

func (h *AnalyticsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/clicks", h.HandleClick)
	mux.HandleFunc("POST /v1/metrics/yesterday", h.HandleMetrics)

	// Compatibility aliases for clients of the first API version.
	mux.HandleFunc("POST /click", h.HandleClick)
	mux.HandleFunc("POST /author-metrics", h.HandleMetrics)
}

func (h *AnalyticsHandler) HandleClick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return
	}

	var req struct {
		UserID   int64 `json:"user_id"`
		AuthorID int64 `json:"author_id"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	if err := h.service.TrackClick(ctx, req.UserID, req.AuthorID); err != nil {
		h.handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *AnalyticsHandler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return
	}

	var req struct {
		AuthorIDs []int64 `json:"author_ids"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.AuthorIDs) > h.maxAuthors {
		writeError(w, http.StatusBadRequest, "too_many_authors", "too many author_ids in one request")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	metrics, err := h.service.GetYesterdayMetrics(ctx, req.AuthorIDs)
	if err != nil {
		h.handleServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics})
}

func (h *AnalyticsHandler) handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidUserID),
		errors.Is(err, service.ErrInvalidAuthorID),
		errors.Is(err, service.ErrNoAuthorIDs):
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "timeout", "request processing timed out")
	case errors.Is(err, context.Canceled):
		return
	default:
		h.logger.Error("request processing failed", "error", err, "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errors.New("request body is too large")
		}
		return errors.New("invalid JSON body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}
