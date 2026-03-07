package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// noopHandler is an http.Handler that does nothing — used to terminate the
// middleware chain in tests.
var noopHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

// executeMiddleware runs the given middleware around noopHandler and returns
// the recorded response.
func executeMiddleware(t *testing.T, mw func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mw(noopHandler).ServeHTTP(rec, req)
	return rec
}

func TestNewSecurityHeaders_Defaults(t *testing.T) {
	rec := executeMiddleware(t, NewSecurityHeaders())
	h := rec.Header()

	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", h.Get("Referrer-Policy"))
	assert.Equal(t, "camera=(), microphone=(), geolocation=()", h.Get("Permissions-Policy"))
	assert.Equal(t, "0", h.Get("X-XSS-Protection"))
	assert.Empty(t, h.Get("Strict-Transport-Security"), "HSTS must not be set by default")
}

func TestNewSecurityHeaders_WithHSTS(t *testing.T) {
	rec := executeMiddleware(t, NewSecurityHeaders(WithHSTS(true)))
	assert.Equal(t, "max-age=63072000; includeSubDomains", rec.Header().Get("Strict-Transport-Security"))
}

func TestNewSecurityHeaders_WithoutHSTS(t *testing.T) {
	// Explicitly passing WithHSTS(false) must also produce no HSTS header.
	rec := executeMiddleware(t, NewSecurityHeaders(WithHSTS(false)))
	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"))
}

func TestNewSecurityHeaders_CustomHeaders(t *testing.T) {
	custom := map[string]string{
		"X-Custom-Header": "custom-value",
	}
	rec := executeMiddleware(t, NewSecurityHeaders(WithCustomHeaders(custom)))
	h := rec.Header()

	assert.Equal(t, "custom-value", h.Get("X-Custom-Header"))
	// Default headers must still be present.
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
}

func TestNewSecurityHeaders_CustomOverridesDefault(t *testing.T) {
	custom := map[string]string{
		"X-Frame-Options": "SAMEORIGIN",
	}
	rec := executeMiddleware(t, NewSecurityHeaders(WithCustomHeaders(custom)))
	assert.Equal(t, "SAMEORIGIN", rec.Header().Get("X-Frame-Options"))
}

func TestNewSecurityHeaders_ReferrerPolicy(t *testing.T) {
	rec := executeMiddleware(t, NewSecurityHeaders(WithReferrerPolicy("no-referrer")))
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
}

func TestNewSecurityHeaders_PermissionsPolicy(t *testing.T) {
	rec := executeMiddleware(t, NewSecurityHeaders(WithPermissionsPolicy("geolocation=(self)")))
	assert.Equal(t, "geolocation=(self)", rec.Header().Get("Permissions-Policy"))
}

func TestSecurityHeaders_BackwardsCompat(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	SecurityHeaders(noopHandler).ServeHTTP(rec, req)
	h := rec.Header()

	// Core headers must still be present.
	assert.Equal(t, "nosniff", h.Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", h.Get("X-Frame-Options"))

	// HSTS must be included for backwards compatibility.
	assert.Equal(t, "max-age=63072000; includeSubDomains", h.Get("Strict-Transport-Security"))
}
