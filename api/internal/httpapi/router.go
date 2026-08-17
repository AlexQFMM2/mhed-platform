package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/AlexQFMM2/mhed-platform/api/internal/config"
	"github.com/AlexQFMM2/mhed-platform/api/internal/game/mh3g"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

type databasePinger interface {
	Ping(context.Context) error
}

type RouterOption func(*apiServer)

func WithConfig(value config.Config) RouterOption {
	return func(server *apiServer) { server.config = value }
}
func WithGameData(value *mh3g.Adapter, openError error) RouterOption {
	return func(server *apiServer) { server.game = value; server.gameError = openError; server.requireGame = true }
}

func NewRouter(logger *slog.Logger, database databasePinger, options ...RouterOption) http.Handler {
	server := newAPIServer(logger)
	if pool, ok := database.(*pgxpool.Pool); ok {
		server.pool = pool
	}
	for _, option := range options {
		option(server)
	}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Recoverer)
	router.Use(requestLogger(logger))

	router.Get("/health/live", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/health/ready", func(writer http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := database.Ping(ctx); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		if server.requireGame {
			if server.gameError != nil || server.game == nil {
				writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "game_data_unavailable"})
				return
			}
			if err := server.game.Ping(ctx); err != nil {
				writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "game_data_unavailable"})
				return
			}
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
	})
	router.Get("/v1/meta", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{
			"service":     "mhed-api",
			"api_version": "v1",
		})
	})
	server.routes(router)

	return router
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			started := time.Now()
			next.ServeHTTP(writer, request)
			logger.Info("http request",
				"request_id", middleware.GetReqID(request.Context()),
				"method", request.Method,
				"path", request.URL.Path,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}
