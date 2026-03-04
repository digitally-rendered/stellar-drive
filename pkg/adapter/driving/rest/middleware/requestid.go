package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// contextKey is an unexported type used for context keys in this package to
// avoid collisions with keys from other packages.
type contextKey string

const requestIDKey contextKey = "request_id"

// RequestID is an HTTP middleware that ensures every request carries a unique
// identifier. It reads the X-Request-ID header; if absent it generates a
// random 16-byte (32 hex character) UUID substitute. The ID is stored in the
// request context and echoed back in the X-Request-ID response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = generateID()
		}

		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext retrieves the request ID stored in ctx by the RequestID
// middleware. Returns an empty string when no ID is present.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// generateID returns a random 32-character hex string suitable for use as a
// request identifier.
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// rand.Read should never fail on modern systems; fall back to a fixed
		// placeholder so the middleware does not panic.
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}
