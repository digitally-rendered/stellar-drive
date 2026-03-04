package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// UserContextKey is the key under which decoded JWT claims are stored in the
// request context. It uses the package-private contextKey type defined in
// requestid.go to avoid collisions with keys from other packages.
const UserContextKey contextKey = "user"

// Auth returns an HTTP middleware that enforces JWT Bearer authentication.
// It extracts the token from the Authorization header, validates the HS256
// signature using secret, checks the expiration claim, and stores the decoded
// claims in the request context under UserContextKey.
//
// Requests that fail authentication receive 401 Unauthorized with a JSON body.
// Requests that carry valid tokens continue to the next handler.
//
// No external JWT library is used. The implementation validates tokens whose
// header specifies alg=HS256 only; tokens with any other algorithm are
// rejected.
func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := extractBearerToken(r)
			if !ok {
				writeUnauthorized(w, "missing or malformed Authorization header")
				return
			}

			claims, err := validateHS256JWT(raw, secret)
			if err != nil {
				writeUnauthorized(w, err.Error())
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext retrieves the JWT claims map stored by the Auth middleware.
// The second return value is false if no claims are present in ctx.
func UserFromContext(ctx context.Context) (map[string]any, bool) {
	v := ctx.Value(UserContextKey)
	if v == nil {
		return nil, false
	}
	claims, ok := v.(map[string]any)
	return claims, ok
}

// extractBearerToken returns the token string from an "Authorization: Bearer
// <token>" header. The second return value is false if the header is absent or
// does not follow the Bearer scheme.
func extractBearerToken(r *http.Request) (string, bool) {
	hdr := r.Header.Get("Authorization")
	if hdr == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(hdr, prefix) {
		return "", false
	}
	token := strings.TrimSpace(hdr[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// validateHS256JWT parses and validates a compact-serialised JWT whose
// algorithm must be HS256. It returns the payload claims on success, or a
// descriptive error on any validation failure.
//
// Validation steps:
//  1. Split into exactly three dot-separated segments (header.payload.signature).
//  2. Base64url-decode and JSON-parse the header; confirm alg=HS256.
//  3. Compute HMAC-SHA256 of "header.payload" using secret; compare with the
//     decoded signature using a constant-time comparison.
//  4. Base64url-decode and JSON-parse the payload.
//  5. Check the "exp" claim if present; reject expired tokens.
func validateHS256JWT(token, secret string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, jwtError("token must have three segments")
	}

	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, jwtError("invalid header encoding")
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, jwtError("invalid header JSON")
	}
	alg, _ := header["alg"].(string)
	if !strings.EqualFold(alg, "HS256") {
		return nil, jwtError("unsupported algorithm: only HS256 is accepted")
	}

	// Verify signature over "header.payload".
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput)) //nolint:errcheck
	expectedSig := mac.Sum(nil)

	providedSig, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, jwtError("invalid signature encoding")
	}
	if !hmac.Equal(expectedSig, providedSig) {
		return nil, jwtError("signature verification failed")
	}

	// Decode payload claims.
	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, jwtError("invalid payload encoding")
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, jwtError("invalid payload JSON")
	}

	// Validate expiration.
	if exp, ok := claims["exp"]; ok {
		var expUnix float64
		switch v := exp.(type) {
		case float64:
			expUnix = v
		case json.Number:
			expUnix, _ = v.Float64()
		}
		if expUnix > 0 && time.Now().Unix() > int64(expUnix) {
			return nil, jwtError("token has expired")
		}
	}

	return claims, nil
}

// base64URLDecode decodes a raw (no-padding) base64url-encoded string.
func base64URLDecode(s string) ([]byte, error) {
	// Restore standard padding so the standard decoder can be used.
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}

// writeUnauthorized writes a 401 JSON response with the given message.
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	// Errors from writing a simple JSON body are intentionally ignored; there
	// is nothing useful the handler can do if the connection is broken.
	w.Write([]byte(`{"error":"` + jsonEscapeString(message) + `"}`)) //nolint:errcheck
}

// jsonEscapeString escapes special characters in s so it can be safely
// embedded in a JSON string literal without using the json package.
func jsonEscapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// jwtError wraps a plain string into an error, scoping it with a consistent
// "auth: jwt: " prefix so callers can distinguish JWT errors from others.
type jwtErrorType struct{ msg string }

func (e *jwtErrorType) Error() string { return "auth: jwt: " + e.msg }

func jwtError(msg string) error { return &jwtErrorType{msg: msg} }
