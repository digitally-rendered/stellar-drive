package rest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// DecodeBody reads the request body and decodes it as a JSON object into a
// map[string]any. Returns an error when the body is absent or malformed.
func DecodeBody(r *http.Request) (map[string]any, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("request body is required")
	}
	defer r.Body.Close()

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	return data, nil
}

// DecodeBulkBody reads the request body and decodes it as a JSON array of
// objects into []map[string]any. Returns an error when the body is absent or
// malformed.
func DecodeBulkBody(r *http.Request) ([]map[string]any, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("request body is required")
	}
	defer r.Body.Close()

	var items []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode bulk request body: %w", err)
	}
	return items, nil
}

// ParseQueryParams extracts filter, sort, pagination, and projection from the
// URL query string and assembles them into a *query.Query.
//
//   - where  — JSON object parsed via query.Parse
//   - sort   — comma-separated field list parsed via query.ParseSortString
//   - limit  — integer page size
//   - offset — integer skip count
//   - cursor — opaque pagination cursor
//   - fields — comma-separated projection field list
func ParseQueryParams(r *http.Request) (*query.Query, error) {
	q := &query.Query{}
	params := r.URL.Query()

	// --- filter ---
	if raw := params.Get("where"); raw != "" {
		filter, err := query.Parse(json.RawMessage(raw))
		if err != nil {
			return nil, fmt.Errorf("parse where param: %w", err)
		}
		q.Filter = filter
	}

	// --- sort ---
	if sortStr := params.Get("sort"); sortStr != "" {
		q.Sort = query.ParseSortString(sortStr)
	}

	// --- pagination ---
	if limitStr := params.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			return nil, fmt.Errorf("parse limit param: %w", err)
		}
		q.Limit = limit
	}

	if offsetStr := params.Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil {
			return nil, fmt.Errorf("parse offset param: %w", err)
		}
		q.Offset = offset
	}

	if cursor := params.Get("cursor"); cursor != "" {
		q.Cursor = cursor
	}

	// --- field projection ---
	if fieldsStr := params.Get("fields"); fieldsStr != "" {
		fields := strings.Split(fieldsStr, ",")
		out := fields[:0]
		for _, f := range fields {
			if trimmed := strings.TrimSpace(f); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		q.Fields = out
	}

	return q, nil
}

// ParsePagination extracts limit, offset, cursor, after, and before from the
// URL query string and returns a normalised PaginationParams.
func ParsePagination(r *http.Request) model.PaginationParams {
	params := r.URL.Query()
	p := model.PaginationParams{}

	if v := params.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.Limit = n
		}
	}
	if v := params.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.Offset = n
		}
	}
	p.Cursor = params.Get("cursor")
	p.After = params.Get("after")
	p.Before = params.Get("before")

	p.Normalize()
	return p
}
