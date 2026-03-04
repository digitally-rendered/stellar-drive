package sql

import (
	"fmt"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// auditColumns is the set of top-level versioning/audit column names that map
// directly to table columns rather than being stored inside the JSON data
// column.
var auditColumns = map[string]bool{
	"id":             true,
	"entity_id":      true,
	"schema_name":    true,
	"record_version": true,
	"schema_version": true,
	"created_at":     true,
	"updated_at":     true,
	"deleted_at":     true,
	"created_by":     true,
	"updated_by":     true,
	"deleted_by":     true,
	"etag":           true,
}

// columnExpr returns the SQL expression for a user-facing field name.
// Audit/versioning columns are referenced by their bare column name; all other
// fields are extracted from the JSON data column using the dialect-appropriate
// extraction syntax.
func columnExpr(field string, d Dialect) string {
	if auditColumns[field] {
		return field
	}
	return d.JSONExtract("data", field)
}

// placeholderList builds a comma-separated list of N placeholder tokens
// starting at position start (1-indexed). E.g. for postgres with start=3, n=3
// it returns "$3,$4,$5".
func placeholderList(d Dialect, start, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = d.Placeholder(start + i)
	}
	return strings.Join(parts, ", ")
}

// counter is a simple mutable int used to track the current bind-parameter
// index during recursive filter translation. It is passed by pointer so that
// all recursive calls share a single monotonically increasing counter.
type counter struct{ n int }

func (c *counter) next(d Dialect) string {
	c.n++
	return d.Placeholder(c.n)
}

// TranslateFilter converts a query.FilterNode tree into a SQL WHERE clause
// fragment (without the "WHERE" keyword) and the corresponding ordered slice
// of bind values. Returns ("", nil) for a nil node.
//
// User data fields are extracted from the JSON data column using the supplied
// dialect. Audit columns (entity_id, created_at, etc.) reference their table
// columns directly.
func TranslateFilter(node *query.FilterNode, d Dialect) (string, []any) {
	if node == nil {
		return "", nil
	}
	c := &counter{}
	clause, args := translateNode(node, d, c)
	return clause, args
}

func translateNode(node *query.FilterNode, d Dialect, c *counter) (string, []any) {
	if node == nil {
		return "", nil
	}
	if node.IsLogical() {
		return translateLogical(node, d, c)
	}
	return translateLeaf(node, d, c)
}

// translateLogical handles _and, _or, _not nodes.
func translateLogical(node *query.FilterNode, d Dialect, c *counter) (string, []any) {
	if len(node.Children) == 0 {
		return "", nil
	}

	var (
		parts []string
		args  []any
	)

	for _, child := range node.Children {
		clause, childArgs := translateNode(child, d, c)
		if clause == "" {
			continue
		}
		parts = append(parts, "("+clause+")")
		args = append(args, childArgs...)
	}

	if len(parts) == 0 {
		return "", nil
	}

	switch node.Operator {
	case query.OpAnd:
		return strings.Join(parts, " AND "), args
	case query.OpOr:
		return strings.Join(parts, " OR "), args
	case query.OpNot:
		// NOT (a AND b AND …)
		inner := strings.Join(parts, " AND ")
		return "NOT (" + inner + ")", args
	default:
		// Unreachable if IsLogical() is correct, but be defensive.
		return strings.Join(parts, " AND "), args
	}
}

// translateLeaf translates a single comparison FilterNode into a SQL expression
// and its bind values.
func translateLeaf(node *query.FilterNode, d Dialect, c *counter) (string, []any) {
	col := columnExpr(node.Field, d)

	switch node.Operator {
	case query.OpEq:
		p := c.next(d)
		return fmt.Sprintf("%s = %s", col, p), []any{node.Value}

	case query.OpNeq:
		p := c.next(d)
		return fmt.Sprintf("%s <> %s", col, p), []any{node.Value}

	case query.OpGt:
		p := c.next(d)
		return fmt.Sprintf("%s > %s", col, p), []any{node.Value}

	case query.OpGte:
		p := c.next(d)
		return fmt.Sprintf("%s >= %s", col, p), []any{node.Value}

	case query.OpLt:
		p := c.next(d)
		return fmt.Sprintf("%s < %s", col, p), []any{node.Value}

	case query.OpLte:
		p := c.next(d)
		return fmt.Sprintf("%s <= %s", col, p), []any{node.Value}

	case query.OpIn:
		vals := toSlice(node.Value)
		if len(vals) == 0 {
			// Nothing can match an empty IN list.
			return "1 = 0", nil
		}
		start := c.n + 1
		args := make([]any, len(vals))
		for i, v := range vals {
			args[i] = v
			c.n++
		}
		return fmt.Sprintf("%s IN (%s)", col, placeholderList(d, start, len(vals))), args

	case query.OpNin:
		vals := toSlice(node.Value)
		if len(vals) == 0 {
			// NOT IN () is always true; represent as a tautology.
			return "1 = 1", nil
		}
		start := c.n + 1
		args := make([]any, len(vals))
		for i, v := range vals {
			args[i] = v
			c.n++
		}
		return fmt.Sprintf("%s NOT IN (%s)", col, placeholderList(d, start, len(vals))), args

	case query.OpLike:
		p := c.next(d)
		return fmt.Sprintf("%s LIKE %s", col, p), []any{node.Value}

	case query.OpILike:
		// ILIKE is native in PostgreSQL; for SQLite we fall back to
		// LIKE which is case-insensitive for ASCII by default.
		p := c.next(d)
		if _, ok := d.(PostgresDialect); ok {
			return fmt.Sprintf("%s ILIKE %s", col, p), []any{node.Value}
		}
		return fmt.Sprintf("%s LIKE %s", col, p), []any{node.Value}

	case query.OpContains:
		// Substring match via LIKE with surrounding % wildcards.
		p := c.next(d)
		v := fmt.Sprintf("%%%v%%", node.Value)
		return fmt.Sprintf("%s LIKE %s", col, p), []any{v}

	case query.OpStartsWith:
		p := c.next(d)
		v := fmt.Sprintf("%v%%", node.Value)
		return fmt.Sprintf("%s LIKE %s", col, p), []any{v}

	case query.OpEndsWith:
		p := c.next(d)
		v := fmt.Sprintf("%%%v", node.Value)
		return fmt.Sprintf("%s LIKE %s", col, p), []any{v}

	case query.OpExists:
		exists, _ := node.Value.(bool)
		if auditColumns[node.Field] {
			// Audit columns always exist; treat as tautology / contradiction.
			if exists {
				return "1 = 1", nil
			}
			return "1 = 0", nil
		}
		// For JSON fields, check whether the extracted value IS NOT NULL.
		if exists {
			return fmt.Sprintf("%s IS NOT NULL", col), nil
		}
		return fmt.Sprintf("%s IS NULL", col), nil

	case query.OpIsNull:
		isNull, _ := node.Value.(bool)
		if isNull {
			return fmt.Sprintf("%s IS NULL", col), nil
		}
		return fmt.Sprintf("%s IS NOT NULL", col), nil

	default:
		// Return a safe tautology for unknown operators so queries do not
		// blow up entirely; callers should validate operators before reaching
		// this point.
		return "1 = 1", nil
	}
}

// toSlice coerces the value into a []any, handling the common cases where the
// value is already []any or a typed slice.
func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case []int:
		out := make([]any, len(t))
		for i, n := range t {
			out[i] = n
		}
		return out
	case []int64:
		out := make([]any, len(t))
		for i, n := range t {
			out[i] = n
		}
		return out
	case []float64:
		out := make([]any, len(t))
		for i, f := range t {
			out[i] = f
		}
		return out
	default:
		// Wrap scalar in a one-element slice as a best-effort fallback.
		return []any{v}
	}
}

// TranslateSort converts a slice of query.SortField into an ORDER BY clause
// fragment (without the "ORDER BY" keyword). Returns an empty string when
// fields is empty.
//
// Audit columns sort by their bare column name; user data fields use the
// dialect-appropriate JSON extraction expression.
func TranslateSort(fields []query.SortField, d Dialect) string {
	if len(fields) == 0 {
		return ""
	}

	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		dir := "ASC"
		if f.Direction == query.SortDesc {
			dir = "DESC"
		}
		parts = append(parts, fmt.Sprintf("%s %s", columnExpr(f.Field, d), dir))
	}

	return strings.Join(parts, ", ")
}
