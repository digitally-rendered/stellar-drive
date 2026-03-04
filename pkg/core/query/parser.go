package query

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxParseDepth = 5

// Parse converts a JSON query body (Hasura-style) into a FilterNode tree.
//
// Top-level fields produce an implicit _and:
//
//	{"name": {"_eq": "doggie"}, "status": {"_in": ["available","pending"]}}
//	=> _and([name _eq "doggie", status _in ["available","pending"]])
//
// Explicit logical operators at the top level are also supported:
//
//	{"_and": [{"name": {"_eq": "doggie"}}, {"age": {"_gt": 2}}]}
//
// Returns an error for unknown operators or excessive nesting depth.
func Parse(raw json.RawMessage) (*FilterNode, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("parse query: expected JSON object: %w", err)
	}

	return parseObject(obj, 0)
}

// parseObject converts a decoded JSON object into a FilterNode tree.
// depth tracks the current recursion level; returns an error when maxParseDepth
// is exceeded.
func parseObject(obj map[string]json.RawMessage, depth int) (*FilterNode, error) {
	if depth >= maxParseDepth {
		return nil, fmt.Errorf("parse query: filter tree exceeds maximum nesting depth of %d", maxParseDepth)
	}

	// Fast path: empty object.
	if len(obj) == 0 {
		return nil, nil
	}

	// Collect top-level filter nodes.
	var children []*FilterNode

	for key, rawVal := range obj {
		op := Operator(key)

		if IsLogicalOperator(op) {
			// Handle top-level logical operators: _and, _or, _not.
			node, err := parseLogical(op, rawVal, depth)
			if err != nil {
				return nil, err
			}
			children = append(children, node)
			continue
		}

		if strings.HasPrefix(key, "_") {
			return nil, fmt.Errorf("parse query: unknown operator %q", key)
		}

		// Regular field key — value must be an object of {operator: value}.
		node, err := parseFieldConditions(key, rawVal, depth)
		if err != nil {
			return nil, err
		}
		children = append(children, node...)
	}

	if len(children) == 0 {
		return nil, nil
	}
	if len(children) == 1 {
		return children[0], nil
	}

	// Wrap multiple conditions in an implicit _and.
	return &FilterNode{
		Operator: OpAnd,
		Children: children,
	}, nil
}

// parseFieldConditions parses a single field's operator map, e.g.
// field: {"_eq": "foo", "_neq": "bar"} -> two FilterNodes.
func parseFieldConditions(field string, rawVal json.RawMessage, depth int) ([]*FilterNode, error) {
	var ops map[string]json.RawMessage
	if err := json.Unmarshal(rawVal, &ops); err != nil {
		// Shorthand: field: <scalar> is treated as field _eq <scalar>.
		var scalar any
		if err2 := json.Unmarshal(rawVal, &scalar); err2 != nil {
			return nil, fmt.Errorf("parse query: field %q: %w", field, err2)
		}
		return []*FilterNode{{Field: field, Operator: OpEq, Value: scalar}}, nil
	}

	var nodes []*FilterNode
	for opKey, opVal := range ops {
		op := Operator(opKey)
		if !IsValidOperator(op) {
			return nil, fmt.Errorf("parse query: field %q: unknown operator %q", field, opKey)
		}
		if IsLogicalOperator(op) {
			return nil, fmt.Errorf("parse query: field %q: logical operator %q cannot appear inside a field condition", field, opKey)
		}

		var value any
		if err := json.Unmarshal(opVal, &value); err != nil {
			return nil, fmt.Errorf("parse query: field %q operator %q: %w", field, opKey, err)
		}

		nodes = append(nodes, &FilterNode{
			Field:    field,
			Operator: op,
			Value:    value,
		})
	}
	return nodes, nil
}

// parseLogical handles _and / _or (array of objects) and _not (single object).
func parseLogical(op Operator, rawVal json.RawMessage, depth int) (*FilterNode, error) {
	if depth >= maxParseDepth {
		return nil, fmt.Errorf("parse query: filter tree exceeds maximum nesting depth of %d", maxParseDepth)
	}

	switch op {
	case OpAnd, OpOr:
		var items []json.RawMessage
		if err := json.Unmarshal(rawVal, &items); err != nil {
			return nil, fmt.Errorf("parse query: operator %q expects an array: %w", op, err)
		}

		children := make([]*FilterNode, 0, len(items))
		for i, item := range items {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(item, &obj); err != nil {
				return nil, fmt.Errorf("parse query: operator %q item %d: expected object: %w", op, i, err)
			}
			child, err := parseObject(obj, depth+1)
			if err != nil {
				return nil, err
			}
			if child != nil {
				children = append(children, child)
			}
		}
		return &FilterNode{Operator: op, Children: children}, nil

	case OpNot:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(rawVal, &obj); err != nil {
			return nil, fmt.Errorf("parse query: operator _not expects an object: %w", err)
		}
		inner, err := parseObject(obj, depth+1)
		if err != nil {
			return nil, err
		}
		var children []*FilterNode
		if inner != nil {
			children = []*FilterNode{inner}
		}
		return &FilterNode{Operator: OpNot, Children: children}, nil

	default:
		return nil, fmt.Errorf("parse query: unexpected logical operator %q", op)
	}
}

// ParseSortString parses a comma-separated sort expression into []SortField.
// A leading "+" or no prefix indicates ascending order; "-" indicates descending.
//
// Example: "+name,-created_at" -> [{name asc}, {created_at desc}]
func ParseSortString(s string) []SortField {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	fields := make([]SortField, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		dir := SortAsc
		switch part[0] {
		case '+':
			part = part[1:]
		case '-':
			dir = SortDesc
			part = part[1:]
		}

		if part == "" {
			continue
		}
		fields = append(fields, SortField{Field: part, Direction: dir})
	}
	return fields
}
