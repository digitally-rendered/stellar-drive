package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/internal/ctxkey"
)

// --- helpers -----------------------------------------------------------------

// staticHandler returns a handler that writes a fixed status, Content-Type,
// and body.
func staticHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	})
}

// --- tests -------------------------------------------------------------------

func TestETag_SingleEntity_SetsHeader(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantETag string
	}{
		{
			name:     "top-level entity_id and record_version",
			body:     `{"entity_id":"abc123","record_version":"7","name":"Fido"}`,
			wantETag: `W/"abc123:7"`,
		},
		{
			name:     "data envelope with entity_id and record_version",
			body:     `{"success":true,"data":{"entity_id":"pet-99","record_version":"3","name":"Rex"}}`,
			wantETag: `W/"pet-99:3"`,
		},
		{
			name:     "numeric record_version",
			body:     `{"entity_id":"order-1","record_version":42}`,
			wantETag: `W/"order-1:42"`,
		},
	}

	mw := ETag()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw(staticHandler(http.StatusOK, tt.body))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/1", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tt.wantETag, rr.Header().Get("ETag"))
			assert.Equal(t, tt.body, rr.Body.String())
		})
	}
}

func TestETag_ListResponse_SetsHashETag(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "JSON array",
			body: `[{"entity_id":"1"},{"entity_id":"2"}]`,
		},
		{
			name: "object without entity fields",
			body: `{"items":[{"id":"a"},{"id":"b"}],"total":2}`,
		},
		{
			name: "data envelope containing an array",
			body: `{"success":true,"data":[{"entity_id":"x"}]}`,
		},
	}

	mw := ETag()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw(staticHandler(http.StatusOK, tt.body))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/pet", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			etag := rr.Header().Get("ETag")
			assert.True(t, strings.HasPrefix(etag, `W/"list:`),
				"expected list ETag, got %q", etag)
			// The hash segment should be 16 hex characters.
			inner := strings.TrimSuffix(strings.TrimPrefix(etag, `W/"list:`), `"`)
			assert.Len(t, inner, 16, "sha256 prefix should be 16 hex chars")
		})
	}
}

func TestETag_IfNoneMatch_304(t *testing.T) {
	body := `{"entity_id":"pet-1","record_version":"5"}`
	expectedETag := `W/"pet-1:5"`

	mw := ETag()
	handler := mw(staticHandler(http.StatusOK, body))

	// First request: get the ETag.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	etag := rr.Header().Get("ETag")
	require.Equal(t, expectedETag, etag)

	// Second request with a matching If-None-Match → 304.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/pet/1", nil)
	req2.Header.Set("If-None-Match", etag)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusNotModified, rr2.Code)
	assert.Empty(t, rr2.Body.String(), "304 response must have no body")
}

func TestETag_IfNoneMatch_Wildcard(t *testing.T) {
	body := `{"entity_id":"pet-2","record_version":"1"}`

	mw := ETag()
	handler := mw(staticHandler(http.StatusOK, body))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/2", nil)
	req.Header.Set("If-None-Match", "*")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotModified, rr.Code)
	assert.Empty(t, rr.Body.String())
}

func TestETag_IfNoneMatch_Mismatch(t *testing.T) {
	body := `{"entity_id":"pet-3","record_version":"9"}`
	expectedETag := `W/"pet-3:9"`

	mw := ETag()
	handler := mw(staticHandler(http.StatusOK, body))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/3", nil)
	req.Header.Set("If-None-Match", `W/"pet-3:8"`) // stale ETag
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, expectedETag, rr.Header().Get("ETag"))
	assert.Equal(t, body, rr.Body.String())
}

func TestETag_SkipsErrorResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"400 Bad Request", http.StatusBadRequest},
		{"401 Unauthorized", http.StatusUnauthorized},
		{"403 Forbidden", http.StatusForbidden},
		{"404 Not Found", http.StatusNotFound},
		{"409 Conflict", http.StatusConflict},
		{"422 Unprocessable Entity", http.StatusUnprocessableEntity},
		{"500 Internal Server Error", http.StatusInternalServerError},
		{"503 Service Unavailable", http.StatusServiceUnavailable},
	}

	body := `{"error":{"code":"NOT_FOUND","message":"pet not found"}}`
	mw := ETag()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := mw(staticHandler(tt.status, body))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/99", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.status, rr.Code)
			assert.Empty(t, rr.Header().Get("ETag"),
				"error responses must not carry an ETag header")
			assert.Equal(t, body, rr.Body.String())
		})
	}
}

func TestETag_SkipsNoContent(t *testing.T) {
	mw := ETag()
	handler := mw(staticHandler(http.StatusNoContent, ""))

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pet/1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Empty(t, rr.Header().Get("ETag"), "204 responses must not carry an ETag header")
	assert.Empty(t, rr.Body.String())
}

func TestETag_IfMatch_StoredInContext(t *testing.T) {
	const ifMatchValue = `W/"pet-7:4"`
	var capturedValue string

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := r.Context().Value(ctxkey.IfMatchKey{}).(string)
		capturedValue = v
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"entity_id":"pet-7","record_version":"5"}`))
	})

	mw := ETag()
	handler := mw(inner)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/pet/7", nil)
	req.Header.Set("If-Match", ifMatchValue)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, ifMatchValue, capturedValue,
		"If-Match header must be stored in context under ctxkey.IfMatchKey{}")
}

func TestETag_POST_SkipsIfNoneMatch(t *testing.T) {
	body := `{"entity_id":"pet-10","record_version":"1"}`

	mw := ETag()
	handler := mw(staticHandler(http.StatusCreated, body))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pet", nil)
	req.Header.Set("If-None-Match", "*")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// POST always returns the real response; 304 is not applicable.
	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("ETag"), "POST responses should still receive an ETag")
	assert.Equal(t, body, rr.Body.String())
}

func TestETag_IfNoneMatch_CommaSeparatedList(t *testing.T) {
	body := `{"entity_id":"pet-5","record_version":"2"}`
	matchingETag := `W/"pet-5:2"`
	otherETag := `W/"pet-5:1"`

	mw := ETag()
	handler := mw(staticHandler(http.StatusOK, body))

	// The matching ETag is the second token in a comma-separated list.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/5", nil)
	req.Header.Set("If-None-Match", otherETag+", "+matchingETag)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotModified, rr.Code)
	assert.Empty(t, rr.Body.String())
}

func TestETag_HEAD_SkipsIfNoneMatch(t *testing.T) {
	body := `{"entity_id":"pet-6","record_version":"3"}`
	expectedETag := `W/"pet-6:3"`

	mw := ETag()
	handler := mw(staticHandler(http.StatusOK, body))

	// HEAD with matching If-None-Match should also 304.
	req := httptest.NewRequest(http.MethodHead, "/api/v1/pet/6", nil)
	req.Header.Set("If-None-Match", expectedETag)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotModified, rr.Code)
	assert.Empty(t, rr.Body.String())
}

func TestETag_NoIfMatchHeader_ContextUnset(t *testing.T) {
	var capturedValue any
	var wasPresent bool

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedValue = r.Context().Value(ctxkey.IfMatchKey{})
		wasPresent = capturedValue != nil
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"entity_id":"x","record_version":"1"}`))
	})

	mw := ETag()
	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pet/1", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.False(t, wasPresent,
		"ctxkey.IfMatchKey{} must not be set when If-Match header is absent")
}
