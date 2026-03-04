package jsonutil

import (
	"fmt"
	"strconv"
	"strings"
)

// ErrPointerSyntax is returned when the pointer string is syntactically invalid
// (i.e. non-empty and does not start with "/").
type ErrPointerSyntax struct {
	Pointer string
}

func (e *ErrPointerSyntax) Error() string {
	return fmt.Sprintf("jsonutil: invalid JSON Pointer %q: must be empty or start with '/'", e.Pointer)
}

// ErrPointerKeyNotFound is returned when a map key referenced by the pointer
// does not exist in the document.
type ErrPointerKeyNotFound struct {
	Pointer string
	Key     string
}

func (e *ErrPointerKeyNotFound) Error() string {
	return fmt.Sprintf("jsonutil: key %q not found (pointer %q)", e.Key, e.Pointer)
}

// ErrPointerIndexOutOfRange is returned when an array index in the pointer is
// outside the bounds of the slice.
type ErrPointerIndexOutOfRange struct {
	Pointer string
	Index   int
	Len     int
}

func (e *ErrPointerIndexOutOfRange) Error() string {
	return fmt.Sprintf("jsonutil: index %d out of range [0,%d) (pointer %q)", e.Index, e.Len, e.Pointer)
}

// ErrPointerNotTraversable is returned when a pointer token attempts to
// traverse into a value that is neither a map nor a slice.
type ErrPointerNotTraversable struct {
	Pointer string
	Token   string
}

func (e *ErrPointerNotTraversable) Error() string {
	return fmt.Sprintf("jsonutil: cannot traverse token %q in pointer %q: value is not an object or array", e.Token, e.Pointer)
}

// Resolve resolves a JSON Pointer (RFC 6901) against doc and returns the
// referenced value. doc must be composed of map[string]any and []any values,
// as produced by encoding/json Unmarshal with an any destination.
//
// An empty pointer ("") returns doc itself.
//
// Token escape sequences are handled per RFC 6901:
//   - "~1" → "/"
//   - "~0" → "~"
//
// Examples:
//
//	Resolve(doc, "")           // returns doc
//	Resolve(doc, "/foo")       // returns doc["foo"]
//	Resolve(doc, "/foo/bar")   // returns doc["foo"]["bar"]
//	Resolve(doc, "/arr/0")     // returns doc["arr"][0]
func Resolve(doc any, pointer string) (any, error) {
	if pointer == "" {
		return doc, nil
	}

	if !strings.HasPrefix(pointer, "/") {
		return nil, &ErrPointerSyntax{Pointer: pointer}
	}

	// Split into tokens, dropping the leading empty string from the first "/".
	rawTokens := strings.Split(pointer[1:], "/")
	tokens := make([]string, len(rawTokens))
	for i, t := range rawTokens {
		tokens[i] = unescapeToken(t)
	}

	current := doc
	for _, token := range tokens {
		switch node := current.(type) {
		case map[string]any:
			val, ok := node[token]
			if !ok {
				return nil, &ErrPointerKeyNotFound{Pointer: pointer, Key: token}
			}
			current = val

		case []any:
			idx, err := strconv.Atoi(token)
			if err != nil {
				return nil, &ErrPointerNotTraversable{Pointer: pointer, Token: token}
			}
			if idx < 0 || idx >= len(node) {
				return nil, &ErrPointerIndexOutOfRange{Pointer: pointer, Index: idx, Len: len(node)}
			}
			current = node[idx]

		default:
			return nil, &ErrPointerNotTraversable{Pointer: pointer, Token: token}
		}
	}

	return current, nil
}

// unescapeToken applies RFC 6901 token unescaping: "~1" → "/" then "~0" → "~".
// The order matters — replace ~1 before ~0 to avoid double-unescaping.
func unescapeToken(token string) string {
	// Fast path: no escapes present.
	if !strings.ContainsRune(token, '~') {
		return token
	}
	token = strings.ReplaceAll(token, "~1", "/")
	token = strings.ReplaceAll(token, "~0", "~")
	return token
}
