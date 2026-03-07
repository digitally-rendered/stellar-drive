package middleware

import "net/http"

// SecurityHeadersOption configures the security headers middleware.
type SecurityHeadersOption func(*securityConfig)

type securityConfig struct {
	headers map[string]string
}

// defaultSecurityHeaders returns the baseline set of security headers applied
// by NewSecurityHeaders when no options are provided.
func defaultSecurityHeaders() map[string]string {
	return map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"Permissions-Policy":     "camera=(), microphone=(), geolocation=()",
		"X-XSS-Protection":       "0",
	}
}

// WithHSTS enables or disables the Strict-Transport-Security header.
// When enable is true the header is set to "max-age=63072000; includeSubDomains".
// When enable is false any previously set HSTS value is removed.
func WithHSTS(enable bool) SecurityHeadersOption {
	return func(c *securityConfig) {
		if enable {
			c.headers["Strict-Transport-Security"] = "max-age=63072000; includeSubDomains"
		} else {
			delete(c.headers, "Strict-Transport-Security")
		}
	}
}

// WithCustomHeaders merges additional headers into the configuration.
// Keys that match a default header name override the default value.
func WithCustomHeaders(headers map[string]string) SecurityHeadersOption {
	return func(c *securityConfig) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

// WithReferrerPolicy overrides the Referrer-Policy header value.
func WithReferrerPolicy(policy string) SecurityHeadersOption {
	return func(c *securityConfig) {
		c.headers["Referrer-Policy"] = policy
	}
}

// WithPermissionsPolicy overrides the Permissions-Policy header value.
func WithPermissionsPolicy(policy string) SecurityHeadersOption {
	return func(c *securityConfig) {
		c.headers["Permissions-Policy"] = policy
	}
}

// NewSecurityHeaders returns a configured security-headers middleware.
// Default headers applied when no options are provided:
//
//	X-Content-Type-Options:  nosniff
//	X-Frame-Options:         DENY
//	Referrer-Policy:         strict-origin-when-cross-origin
//	Permissions-Policy:      camera=(), microphone=(), geolocation=()
//	X-XSS-Protection:        0
//
// Options are applied in the order they are given, so later options win.
func NewSecurityHeaders(opts ...SecurityHeadersOption) func(http.Handler) http.Handler {
	cfg := &securityConfig{
		headers: defaultSecurityHeaders(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Snapshot the resolved header map so the closure is immutable after
	// construction. This avoids data races if the caller retains the opts
	// slice and modifies any captured maps after calling NewSecurityHeaders.
	snapshot := make(map[string]string, len(cfg.headers))
	for k, v := range cfg.headers {
		snapshot[k] = v
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range snapshot {
				w.Header().Set(k, v)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders is the backwards-compatible bare middleware. It applies the
// default security headers plus Strict-Transport-Security (HSTS) to match the
// behaviour of the original implementation.
func SecurityHeaders(next http.Handler) http.Handler {
	return NewSecurityHeaders(WithHSTS(true))(next)
}
