package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pinger struct{ err error }

func (value pinger) Ping(context.Context) error { return value.err }

func TestLive(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()
	NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), pinger{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("live returned %d", response.Code)
	}
}

func TestReadyReportsDatabaseFailure(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	response := httptest.NewRecorder()
	NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), pinger{err: errors.New("offline")}).ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready returned %d", response.Code)
	}
}
