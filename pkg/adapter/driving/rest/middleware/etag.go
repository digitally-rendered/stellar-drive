package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/digitally-rendered/stellar-drive/internal/ctxkey"
)

// responseCapture wraps http.ResponseWriter and buffers the response body so
// the ETag middleware can inspect and hash the payload before forwarding it to
// the underlying writer.
type responseCapture struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
	wrote  bool
}

// WriteHeader captures the status code without forwarding it yet. The actual
// write is deferred until the middleware has computed and set the ETag header.
func (rc *responseCapture) WriteHeader(status int) {
	if !rc.wrote {
		rc.status = status
		rc.wrote = true
	}
}

// Write accumulates body bytes into the internal buffer without writing them
// to the underlying ResponseWriter. The middleware flushes the buffer after
// computing the ETag.
func (rc *responseCapture) Write(b []byte) (int, error) {
	if !rc.wrote {
		rc.status = http.StatusOK
		rc.wrote = true
	}
	return rc.buf.Write(b)
}

// Flush delegates to the underlying http.Flusher if supported. Because the
// body is buffered, this is a best-effort signal; callers should not rely on
// partial flushes when the ETag middleware is active.
func (rc *responseCapture) Flush() {
	if f, ok := rc.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ETagOption configures the ETag middleware.
type ETagOption func(*etagConfig)

// etagConfig holds the resolved configuration for the ETag middleware.
type etagConfig struct{}

// ETag returns an HTTP middleware that adds ETag headers to responses and
// handles conditional request semantics (If-None-Match, If-Match).
//
// ETag computation:
//   - For single-entity JSON responses that contain entity_id and
//     record_version (at the top level or inside a "data" envelope) the ETag
//     is W/"{entity_id}:{record_version}".
//   - For all other responses (lists, non-JSON bodies) the ETag is
//     W/"list:{sha256(body)[:16]}".
//
// If-None-Match (GET/HEAD only): when the computed ETag weakly matches any
// value in the request header the middleware short-circuits with 304 Not
// Modified and an empty body.
//
// If-Match: the header value is stored in the request context under
// ctxkey.IfMatchKey so the service layer can use it for optimistic-concurrency
// checks in pre-event hooks.
//
// ETag computation and If-None-Match short-circuiting are skipped for
// responses with a status code >= 400 or == 204.
func ETag(opts ...ETagOption) func(http.Handler) http.Handler {
	cfg := &etagConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Store If-Match in context before the downstream handler runs so
			// service-layer hooks can read it.
			if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
				ctx := context.WithValue(r.Context(), ctxkey.IfMatchKey{}, ifMatch)
				r = r.WithContext(ctx)
			}

			// Capture the response so we can inspect the body before sending.
			rc := &responseCapture{
				ResponseWriter: w,
				status:         http.StatusOK,
			}
			next.ServeHTTP(rc, r)

			body := rc.buf.Bytes()
			status := rc.status

			// Skip ETag computation for error responses and No Content.
			if status == http.StatusNoContent || status >= http.StatusBadRequest {
				w.WriteHeader(status)
				if len(body) > 0 {
					_, _ = w.Write(body)
				}
				return
			}

			// Compute and set the ETag header.
			etag := computeETag(body)
			w.Header().Set("ETag", etag)

			// If-None-Match handling (GET and HEAD only).
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				if ifNoneMatch := r.Header.Get("If-None-Match"); ifNoneMatch != "" {
					if etagWeakMatch(etag, ifNoneMatch) {
						w.WriteHeader(http.StatusNotModified)
						return
					}
				}
			}

			// Forward the captured response to the real writer.
			w.WriteHeader(status)
			if len(body) > 0 {
				_, _ = w.Write(body)
			}
		})
	}
}

// computeETag derives an ETag value from a response body.
//
// It attempts to parse the body as JSON and looks for entity_id and
// record_version fields at the top level or inside a nested "data" object.
// When both fields are found it returns a deterministic entity ETag of the
// form W/"{entity_id}:{record_version}".
//
// For list responses or bodies where the two required fields are absent it
// falls back to W/"list:{sha256[:16]}".
func computeETag(body []byte) string {
	if len(body) == 0 {
		sum := sha256.Sum256(body)
		return fmt.Sprintf(`W/"list:%x"`, sum[:8])
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		entityID, version, ok := extractEntityFields(payload)
		if !ok {
			// Check inside a "data" envelope.
			if data, ok2 := payload["data"]; ok2 {
				if dataMap, ok3 := data.(map[string]any); ok3 {
					entityID, version, ok = extractEntityFields(dataMap)
				}
			}
		}
		if ok {
			return fmt.Sprintf(`W/"%s:%s"`, entityID, version)
		}
	}

	// Fallback: hash the raw body.
	sum := sha256.Sum256(body)
	return fmt.Sprintf(`W/"list:%x"`, sum[:8])
}

// extractEntityFields returns the entity_id and record_version from a
// map[string]any. Both fields must be present and non-empty for ok to be true.
func extractEntityFields(m map[string]any) (entityID, version string, ok bool) {
	rawID, hasID := m["entity_id"]
	rawVer, hasVer := m["record_version"]
	if !hasID || !hasVer {
		return "", "", false
	}

	entityID = anyToString(rawID)
	version = anyToString(rawVer)
	if entityID == "" || version == "" {
		return "", "", false
	}
	return entityID, version, true
}

// anyToString converts common JSON-decoded value types to their string
// representation. Numbers decoded from JSON become float64 by default, so
// integer-valued floats are formatted without a decimal point.
func anyToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case json.Number:
		return val.String()
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// etagWeakMatch reports whether the computed ETag weakly matches any token in
// the If-None-Match header value. Weak comparison strips the W/ prefix before
// comparing the quoted strings. The wildcard token "*" matches everything.
func etagWeakMatch(etag, ifNoneMatch string) bool {
	computed := stripWeak(etag)

	for _, token := range splitETags(ifNoneMatch) {
		token = strings.TrimSpace(token)
		if token == "*" {
			return true
		}
		if stripWeak(token) == computed {
			return true
		}
	}
	return false
}

// stripWeak removes the W/ prefix from a weak ETag, leaving only the quoted
// string component. If the prefix is absent the value is returned unchanged.
func stripWeak(etag string) string {
	etag = strings.TrimSpace(etag)
	if strings.HasPrefix(etag, `W/`) {
		return etag[2:]
	}
	return etag
}

// splitETags splits a comma-separated If-None-Match (or If-Match) header value
// into individual ETag tokens. Quoted strings may contain commas, so splitting
// on comma alone is not safe; however, RFC 9110 ETags are always simple quoted
// strings without embedded commas, so a naive split is correct here.
func splitETags(header string) []string {
	return strings.Split(header, ",")
}
