package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echoHandler returns the body it received as JSON.
func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		var buf bytes.Buffer
		if r.Body != nil {
			buf.ReadFrom(r.Body)
		}
		if buf.Len() == 0 {
			_, _ = w.Write([]byte(`{"message":"hello"}`))
		} else {
			_, _ = w.Write(buf.Bytes())
		}
	})
}

func TestContentNegotiation(t *testing.T) {
	tests := []struct {
		name            string
		accept          string
		wantContentType string
		wantYAML        bool
		wantXML         bool
	}{
		{
			name:            "json passthrough",
			accept:          "application/json",
			wantContentType: "application/json",
		},
		{
			name:            "yaml response",
			accept:          "application/yaml",
			wantContentType: "application/yaml",
			wantYAML:        true,
		},
		{
			name:            "xml response",
			accept:          "application/xml",
			wantContentType: "application/xml",
			wantXML:         true,
		},
		{
			name:            "wildcard defaults to json",
			accept:          "*/*",
			wantContentType: "application/json",
		},
		{
			name:            "empty accept defaults to json",
			accept:          "",
			wantContentType: "application/json",
		},
		{
			name:            "unsupported accept falls back to json",
			accept:          "text/html",
			wantContentType: "application/json",
		},
	}

	mw := ContentNegotiation()
	handler := mw(echoHandler())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, tt.wantContentType, w.Header().Get("Content-Type"))

			if tt.wantYAML {
				assert.True(t, strings.Contains(w.Body.String(), "message:"), "expected YAML output")
			}
			if tt.wantXML {
				body := w.Body.String()
				assert.True(t, strings.Contains(body, "<?xml") || strings.Contains(body, "<root>"), "expected XML output")
			}
		})
	}
}

func TestContentNegotiation_YAMLRequestBody(t *testing.T) {
	mw := ContentNegotiation()

	// The inner handler verifies it received valid JSON.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data map[string]any
		err := json.NewDecoder(r.Body).Decode(&data)
		require.NoError(t, err)
		assert.Equal(t, "Fido", data["name"])
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	yamlBody := "name: Fido\nage: 3\n"
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestContentNegotiation_NoContent(t *testing.T) {
	mw := ContentNegotiation()

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	handler := mw(inner)

	req := httptest.NewRequest(http.MethodDelete, "/test", nil)
	req.Header.Set("Accept", "application/yaml")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
}
