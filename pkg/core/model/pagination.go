package model

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// PaginationParams holds parsed pagination parameters from the request.
type PaginationParams struct {
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Cursor string `json:"cursor,omitempty"`
	After  string `json:"after,omitempty"`
	Before string `json:"before,omitempty"`
}

const (
	// DefaultLimit is the default page size.
	DefaultLimit = 20
	// MaxLimit is the maximum allowed page size.
	MaxLimit = 100
)

// EncodeCursor encodes an entity ID and record version into a URL-safe base64
// cursor string. The raw format is "entityID:recordVersion".
func EncodeCursor(entityID string, recordVersion int) string {
	raw := fmt.Sprintf("%s:%d", entityID, recordVersion)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor decodes a base64 cursor back into its entity ID and record
// version components. Returns an error if the cursor is malformed.
//
// The raw format is "entityID:recordVersion". We split on the last colon so
// that entity IDs containing colons (e.g. URN-style identifiers) are handled
// correctly.
func DecodeCursor(cursor string) (entityID string, recordVersion int, err error) {
	b, decErr := base64.RawURLEncoding.DecodeString(cursor)
	if decErr != nil {
		return "", 0, fmt.Errorf("decode cursor: %w", decErr)
	}
	raw := string(b)
	idx := strings.LastIndex(raw, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("parse cursor %q: missing colon separator", raw)
	}
	entityID = raw[:idx]
	recordVersion, err = strconv.Atoi(raw[idx+1:])
	if err != nil {
		return "", 0, fmt.Errorf("parse cursor %q: record version: %w", raw, err)
	}
	return entityID, recordVersion, nil
}

// Normalize clamps Limit to [1, MaxLimit] and sets DefaultLimit when zero.
// A non-positive Offset is reset to zero.
func (p *PaginationParams) Normalize() {
	if p.Limit <= 0 {
		p.Limit = DefaultLimit
	}
	if p.Limit > MaxLimit {
		p.Limit = MaxLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
}
