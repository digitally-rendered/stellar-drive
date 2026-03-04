package middleware

import "net/http"

// SecurityHeaders is an HTTP middleware that adds a standard set of security
// headers to every response.
//
//   - X-Content-Type-Options: nosniff            — prevent MIME-type sniffing
//   - X-Frame-Options: DENY                      — disallow embedding in frames
//   - X-XSS-Protection: 1; mode=block            — enable XSS filter in older browsers
//   - Strict-Transport-Security: max-age=31536000; includeSubDomains — enforce HTTPS
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}
