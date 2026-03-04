package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture the status code written
// by the downstream handler.
type responseWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (rw *responseWriter) WriteHeader(status int) {
	if !rw.wrote {
		rw.status = status
		rw.wrote = true
	}
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wrote {
		rw.status = http.StatusOK
		rw.wrote = true
	}
	return rw.ResponseWriter.Write(b)
}

// Logging is an HTTP middleware that logs the HTTP method, path, response
// status code, and request duration using the structured slog package.
// The request ID (if set by the RequestID middleware) is included when present.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}

		if id := RequestIDFromContext(r.Context()); id != "" {
			attrs = append(attrs, "request_id", id)
		}

		slog.Info("http request", attrs...)
	})
}
