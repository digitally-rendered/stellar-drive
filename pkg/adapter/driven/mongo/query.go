package mongo

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// auditFields is the set of top-level document fields that must NOT be
// prefixed with "data." when building a MongoDB filter or sort.
var auditFields = map[string]bool{
	"_id":            true,
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

// fieldKey returns the MongoDB document key for a user-facing field name.
// Fields that are known top-level audit fields are returned as-is; all other
// fields are assumed to live inside the "data" subdocument.
func fieldKey(field string) string {
	if auditFields[field] {
		return field
	}
	return "data." + field
}

// TranslateFilter converts a QueryDSL FilterNode tree into a MongoDB bson.D
// filter document. Returns an error if an unsupported operator is encountered.
func TranslateFilter(node *query.FilterNode) (bson.D, error) {
	if node == nil {
		return bson.D{}, nil
	}

	if node.IsLogical() {
		return translateLogical(node)
	}
	return translateLeaf(node)
}

// translateLogical handles _and, _or, and _not nodes.
func translateLogical(node *query.FilterNode) (bson.D, error) {
	switch node.Operator {
	case query.OpAnd:
		children, err := translateChildren(node.Children)
		if err != nil {
			return nil, err
		}
		return bson.D{{Key: "$and", Value: children}}, nil

	case query.OpOr:
		children, err := translateChildren(node.Children)
		if err != nil {
			return nil, err
		}
		return bson.D{{Key: "$or", Value: children}}, nil

	case query.OpNot:
		// $nor with a single element is the idiomatic single-document negation.
		if len(node.Children) == 0 {
			return bson.D{}, nil
		}
		children, err := translateChildren(node.Children)
		if err != nil {
			return nil, err
		}
		return bson.D{{Key: "$nor", Value: children}}, nil

	default:
		return nil, fmt.Errorf("mongo: unknown logical operator %q", node.Operator)
	}
}

// translateChildren translates a slice of filter nodes into a bson.A suitable
// for use as the value of $and / $or / $nor.
func translateChildren(nodes []*query.FilterNode) (bson.A, error) {
	result := make(bson.A, 0, len(nodes))
	for _, child := range nodes {
		doc, err := TranslateFilter(child)
		if err != nil {
			return nil, err
		}
		result = append(result, doc)
	}
	return result, nil
}

// translateLeaf translates a comparison (leaf) FilterNode into bson.D.
func translateLeaf(node *query.FilterNode) (bson.D, error) {
	key := fieldKey(node.Field)

	switch node.Operator {
	case query.OpEq:
		return bson.D{{Key: key, Value: bson.D{{Key: "$eq", Value: node.Value}}}}, nil

	case query.OpNeq:
		return bson.D{{Key: key, Value: bson.D{{Key: "$ne", Value: node.Value}}}}, nil

	case query.OpGt:
		return bson.D{{Key: key, Value: bson.D{{Key: "$gt", Value: node.Value}}}}, nil

	case query.OpGte:
		return bson.D{{Key: key, Value: bson.D{{Key: "$gte", Value: node.Value}}}}, nil

	case query.OpLt:
		return bson.D{{Key: key, Value: bson.D{{Key: "$lt", Value: node.Value}}}}, nil

	case query.OpLte:
		return bson.D{{Key: key, Value: bson.D{{Key: "$lte", Value: node.Value}}}}, nil

	case query.OpIn:
		return bson.D{{Key: key, Value: bson.D{{Key: "$in", Value: node.Value}}}}, nil

	case query.OpNin:
		return bson.D{{Key: key, Value: bson.D{{Key: "$nin", Value: node.Value}}}}, nil

	case query.OpLike:
		// SQL LIKE: % → .*, _ → .
		pattern := sqlLikeToRegex(fmt.Sprintf("%v", node.Value))
		return bson.D{{Key: key, Value: bson.D{{Key: "$regex", Value: pattern}}}}, nil

	case query.OpILike:
		pattern := sqlLikeToRegex(fmt.Sprintf("%v", node.Value))
		return bson.D{{Key: key, Value: bson.D{
			{Key: "$regex", Value: pattern},
			{Key: "$options", Value: "i"},
		}}}, nil

	case query.OpContains:
		// Literal substring match — escape regex metacharacters.
		pattern := regexp.QuoteMeta(fmt.Sprintf("%v", node.Value))
		return bson.D{{Key: key, Value: bson.D{{Key: "$regex", Value: pattern}}}}, nil

	case query.OpStartsWith:
		pattern := "^" + regexp.QuoteMeta(fmt.Sprintf("%v", node.Value))
		return bson.D{{Key: key, Value: bson.D{{Key: "$regex", Value: pattern}}}}, nil

	case query.OpEndsWith:
		pattern := regexp.QuoteMeta(fmt.Sprintf("%v", node.Value)) + "$"
		return bson.D{{Key: key, Value: bson.D{{Key: "$regex", Value: pattern}}}}, nil

	case query.OpExists:
		exists, _ := node.Value.(bool)
		return bson.D{{Key: key, Value: bson.D{{Key: "$exists", Value: exists}}}}, nil

	case query.OpIsNull:
		isNull, _ := node.Value.(bool)
		if isNull {
			return bson.D{{Key: key, Value: bson.D{{Key: "$eq", Value: nil}}}}, nil
		}
		return bson.D{{Key: key, Value: bson.D{{Key: "$ne", Value: nil}}}}, nil

	default:
		return nil, fmt.Errorf("mongo: unsupported operator %q", node.Operator)
	}
}

// sqlLikeToRegex converts a SQL LIKE pattern (with % and _ wildcards) into
// an equivalent regular expression string.
//
//   - % matches zero or more characters → .*
//   - _ matches exactly one character   → .
//   - All other regex metacharacters are escaped.
func sqlLikeToRegex(like string) string {
	var sb strings.Builder
	sb.Grow(len(like) + 4)

	i := 0
	for i < len(like) {
		ch := like[i]
		switch ch {
		case '%':
			sb.WriteString(".*")
		case '_':
			sb.WriteByte('.')
		case '\\':
			// Escaped wildcard: \% or \_ become literal % or _.
			if i+1 < len(like) {
				next := like[i+1]
				if next == '%' || next == '_' {
					sb.WriteString(regexp.QuoteMeta(string(next)))
					i += 2
					continue
				}
			}
			sb.WriteString(regexp.QuoteMeta(string(ch)))
		default:
			sb.WriteString(regexp.QuoteMeta(string(ch)))
		}
		i++
	}

	return sb.String()
}

// TranslateSort converts a slice of QueryDSL SortFields into a MongoDB bson.D
// sort specification.
//
// Sort fields that reference audit fields (entity_id, created_at, etc.) are
// kept as-is; all other fields are prefixed with "data.".
func TranslateSort(fields []query.SortField) bson.D {
	if len(fields) == 0 {
		return bson.D{}
	}

	sort := make(bson.D, 0, len(fields))
	for _, f := range fields {
		dir := 1
		if f.Direction == query.SortDesc {
			dir = -1
		}
		sort = append(sort, bson.E{Key: fieldKey(f.Field), Value: dir})
	}
	return sort
}
