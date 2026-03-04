package middleware

import (
	"net/http"
	"strings"
)

// CORS returns an HTTP middleware that handles Cross-Origin Resource Sharing.
// allowedOrigins is a list of permitted Origin header values. Pass ["*"] to
// allow all origins.
//
// Preflight OPTIONS requests receive a 204 No Content response with the
// appropriate CORS headers and do not reach the downstream handler.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		originSet[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else if _, ok := originSet[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
				}

				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			// Handle preflight.
			if r.Method == http.MethodOptions {
				requestedMethod := r.Header.Get("Access-Control-Request-Method")
				requestedHeaders := r.Header.Get("Access-Control-Request-Headers")

				if requestedMethod != "" {
					w.Header().Set("Access-Control-Allow-Methods", allowedMethods())
				}
				if requestedHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
				}

				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// allowedMethods returns the default list of HTTP methods permitted for CORS.
func allowedMethods() string {
	return strings.Join([]string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodHead,
	}, ", ")
}
