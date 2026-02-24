package logging

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Setup configures the global slog default with a JSON handler at the level
// specified by the LOG_LEVEL environment variable (debug, info, warn, error).
func Setup() {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})))
}

// responseWriter wraps [http.ResponseWriter] to capture the status code.
type responseWriter struct {
	http.ResponseWriter

	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Middleware is a chi-compatible HTTP request logger.
// It skips /healthz to avoid Kubernetes probe noise.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.status),
			slog.Duration("duration", duration),
		}

		switch {
		case rw.status >= http.StatusInternalServerError:
			slog.LogAttrs(r.Context(), slog.LevelError, "request", attrs...)
		case rw.status >= http.StatusBadRequest:
			slog.LogAttrs(r.Context(), slog.LevelWarn, "request", attrs...)
		default:
			slog.LogAttrs(r.Context(), slog.LevelInfo, "request", attrs...)
		}
	})
}
