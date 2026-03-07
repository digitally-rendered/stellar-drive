package middleware

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ContentNegotiation returns an HTTP middleware that handles content
// negotiation based on the Accept and Content-Type headers.
//
// Request side: If Content-Type is YAML, the body is read, converted to JSON,
// and replaced so downstream handlers always receive JSON.
//
// Response side: If Accept is YAML or XML, the JSON response body is
// re-serialized in the requested format.
func ContentNegotiation() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// --- Request: convert inbound YAML body to JSON ---
			if r.Body != nil && r.ContentLength != 0 {
				ct := parseMediaType(r.Header.Get("Content-Type"))
				if isYAML(ct) {
					body, err := io.ReadAll(r.Body)
					_ = r.Body.Close()
					if err == nil {
						var data any
						if err := yaml.Unmarshal(body, &data); err == nil {
							if jsonBytes, err := json.Marshal(data); err == nil {
								r.Body = io.NopCloser(bytes.NewReader(jsonBytes))
								r.ContentLength = int64(len(jsonBytes))
								r.Header.Set("Content-Type", "application/json")
							}
						}
					}
				}
			}

			// --- Response: re-serialize JSON to requested format ---
			accept := parseMediaType(r.Header.Get("Accept"))

			if isYAML(accept) || isXML(accept) {
				buf := &bufResponseWriter{
					ResponseWriter: w,
					buf:            &bytes.Buffer{},
					status:         http.StatusOK,
				}
				next.ServeHTTP(buf, r)

				// Forward status for empty bodies (e.g. 204 No Content).
				if buf.buf.Len() == 0 {
					w.WriteHeader(buf.status)
					return
				}

				var data any
				if err := json.Unmarshal(buf.buf.Bytes(), &data); err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(buf.status)
					_, _ = w.Write(buf.buf.Bytes())
					return
				}

				if isYAML(accept) {
					out, err := yaml.Marshal(data)
					if err != nil {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(buf.status)
						_, _ = w.Write(buf.buf.Bytes())
						return
					}
					w.Header().Set("Content-Type", "application/yaml")
					w.WriteHeader(buf.status)
					_, _ = w.Write(out)
				} else {
					out := marshalToXML(data)
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(buf.status)
					_, _ = w.Write([]byte(xml.Header))
					_, _ = w.Write(out)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// marshalToXML converts an arbitrary value (decoded from JSON) into XML bytes.
// encoding/xml cannot marshal map[string]any directly, so we manually build
// the XML representation.
func marshalToXML(v any) []byte {
	var buf bytes.Buffer
	writeXMLValue(&buf, "root", v)
	return buf.Bytes()
}

func writeXMLValue(buf *bytes.Buffer, tag string, v any) {
	switch val := v.(type) {
	case map[string]any:
		fmt.Fprintf(buf, "<%s>", tag)
		// Sort keys for deterministic output.
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeXMLValue(buf, k, val[k])
		}
		fmt.Fprintf(buf, "</%s>", tag)
	case []any:
		for _, item := range val {
			writeXMLValue(buf, tag, item)
		}
	case nil:
		fmt.Fprintf(buf, "<%s/>", tag)
	default:
		fmt.Fprintf(buf, "<%s>%v</%s>", tag, val, tag)
	}
}

// bufResponseWriter captures the response body into a buffer instead of
// writing it to the underlying ResponseWriter.
type bufResponseWriter struct {
	http.ResponseWriter
	buf    *bytes.Buffer
	status int
	wrote  bool
}

func (w *bufResponseWriter) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
	}
}

func (w *bufResponseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.buf.Write(b)
}

func parseMediaType(header string) string {
	return strings.TrimSpace(strings.SplitN(header, ";", 2)[0])
}

func isYAML(mt string) bool {
	switch strings.ToLower(mt) {
	case "application/yaml", "application/x-yaml", "text/yaml":
		return true
	}
	return false
}

func isXML(mt string) bool {
	switch strings.ToLower(mt) {
	case "application/xml", "text/xml":
		return true
	}
	return false
}
