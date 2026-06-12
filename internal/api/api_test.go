package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aarrico/gramwise/internal/api"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestHandler(t *testing.T, db api.Pinger) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return api.New(api.Config{Logger: logger, DB: db})
}

func TestHello(t *testing.T) {
	h := newTestHandler(t, fakePinger{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/hello", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if want := "hello!"; body.Message != want {
		t.Errorf("message = %q, want %q", body.Message, want)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("missing X-Request-Id response header")
	}
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name string
		db   api.Pinger
		want int
	}{
		{"db up", fakePinger{}, http.StatusOK},
		{"db down", fakePinger{err: errors.New("connection refused")}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t, tt.db)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRequestIDPassthrough(t *testing.T) {
	h := newTestHandler(t, fakePinger{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	req.Header.Set("X-Request-Id", "abc123")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "abc123" {
		t.Errorf("X-Request-Id = %q, want %q", got, "abc123")
	}
}
