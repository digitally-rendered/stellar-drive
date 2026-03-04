package query

import (
	"fmt"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// MaxDepth is the maximum allowed nesting depth for filter trees.
const MaxDepth = 5

// Validate checks that all fields referenced in the filter tree exist in the
// provided schema definition and that operator/value type combinations are
// compatible.
func Validate(filter *FilterNode, def *schema.SchemaDefinition) error {
	if filter == nil {
		return nil
	}
	if def == nil {
		return fmt.Errorf("validate filter: schema definition is nil")
	}

	// Build a fast field-name lookup (includes audit fields).
	fieldIndex := buildFieldIndex(def)

	if err := validateDepth(filter, 0); err != nil {
		return err
	}
	return validateNode(filter, fieldIndex)
}

// validateDepth checks that the filter tree does not exceed MaxDepth.
func validateDepth(filter *FilterNode, currentDepth int) error {
	if filter == nil {
		return nil
	}
	if currentDepth > MaxDepth {
		return fmt.Errorf("validate filter: nesting depth exceeds maximum of %d", MaxDepth)
	}
	for _, child := range filter.Children {
		if err := validateDepth(child, currentDepth+1); err != nil {
			return err
		}
	}
	return nil
}

// validateNode recursively validates a single FilterNode and its children.
func validateNode(node *FilterNode, fieldIndex map[string]schema.FieldDefinition) error {
	if node == nil {
		return nil
	}

	if !IsValidOperator(node.Operator) {
		return fmt.Errorf("validate filter: unknown operator %q", node.Operator)
	}

	if node.IsLogical() {
		// _not must have exactly one child; _and/_or must have at least one.
		switch node.Operator {
		case OpNot:
			if len(node.Children) != 1 {
				return fmt.Errorf("validate filter: _not requires exactly 1 child, got %d", len(node.Children))
			}
		case OpAnd, OpOr:
			if len(node.Children) == 0 {
				return fmt.Errorf("validate filter: %s requires at least 1 child", node.Operator)
			}
		}
		for _, child := range node.Children {
			if err := validateNode(child, fieldIndex); err != nil {
				return err
			}
		}
		return nil
	}

	// Leaf comparison node: validate field exists and value type is compatible.
	if node.Field == "" {
		return fmt.Errorf("validate filter: comparison node missing field name")
	}

	fieldDef, ok := fieldIndex[node.Field]
	if !ok {
		return fmt.Errorf("validate filter: unknown field %q", node.Field)
	}

	if err := validateValueType(node, fieldDef); err != nil {
		return err
	}

	return nil
}

// validateValueType checks that the node's value is compatible with the field
// type and operator.
func validateValueType(node *FilterNode, field schema.FieldDefinition) error {
	op := node.Operator
	val := node.Value

	// _exists and _is_null accept boolean values.
	if op == OpExists || op == OpIsNull {
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("validate filter: field %q operator %q expects boolean value, got %T", node.Field, op, val)
		}
		return nil
	}

	// _in and _nin expect arrays.
	if op == OpIn || op == OpNin {
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("validate filter: field %q operator %q expects array value, got %T", node.Field, op, val)
		}
		return nil
	}

	// String operators require string fields.
	if op == OpLike || op == OpILike || op == OpContains || op == OpStartsWith || op == OpEndsWith {
		if field.JSONType != "string" && field.JSONType != "" {
			return fmt.Errorf("validate filter: field %q: operator %q is only valid for string fields", node.Field, op)
		}
		if _, ok := val.(string); !ok {
			return fmt.Errorf("validate filter: field %q operator %q expects string value, got %T", node.Field, op, val)
		}
		return nil
	}

	// Comparison operators: check value type matches field type.
	return validateScalarType(node.Field, field.JSONType, val)
}

// validateScalarType verifies that a scalar value is compatible with the JSON Schema type.
func validateScalarType(fieldName string, jsonType string, val any) error {
	if val == nil || jsonType == "" {
		return nil
	}

	switch jsonType {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("validate filter: field %q expects string value, got %T", fieldName, val)
		}
	case "number":
		if !isNumeric(val) {
			return fmt.Errorf("validate filter: field %q expects number value, got %T", fieldName, val)
		}
	case "integer":
		if !isNumeric(val) {
			return fmt.Errorf("validate filter: field %q expects integer value, got %T", fieldName, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("validate filter: field %q expects boolean value, got %T", fieldName, val)
		}
	}
	// array / object / null / any: no strict value-type constraint on filter values.
	return nil
}

// isNumeric reports whether v is a numeric Go type.
func isNumeric(v any) bool {
	switch v.(type) {
	case float32, float64, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	}
	return false
}

// buildFieldIndex creates a map of field name -> FieldDefinition including
// audit fields so they can be used in filters.
func buildFieldIndex(def *schema.SchemaDefinition) map[string]schema.FieldDefinition {
	all := schema.DocumentFields(def) // user fields + audit fields
	idx := make(map[string]schema.FieldDefinition, len(all))
	for _, f := range all {
		idx[f.Name] = f
		// Also index dot-notation paths for nested object fields.
		if len(f.Properties) > 0 {
			for _, nested := range f.Properties {
				idx[f.Name+"."+nested.Name] = nested
			}
		}
	}
	return idx
}

