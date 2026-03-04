package query

// ValidOperators is the allowlist of all permitted query operators.
var ValidOperators = map[Operator]bool{
	OpEq:         true,
	OpNeq:        true,
	OpGt:         true,
	OpGte:        true,
	OpLt:         true,
	OpLte:        true,
	OpIn:         true,
	OpNin:        true,
	OpLike:       true,
	OpILike:      true,
	OpContains:   true,
	OpStartsWith: true,
	OpEndsWith:   true,
	OpExists:     true,
	OpIsNull:     true,
	OpAnd:        true,
	OpOr:         true,
	OpNot:        true,
}

// logicalOperators is the subset of operators that combine child nodes.
var logicalOperators = map[Operator]bool{
	OpAnd: true,
	OpOr:  true,
	OpNot: true,
}

// comparisonOperators is the subset of operators that compare a field to a value.
var comparisonOperators = map[Operator]bool{
	OpEq:         true,
	OpNeq:        true,
	OpGt:         true,
	OpGte:        true,
	OpLt:         true,
	OpLte:        true,
	OpIn:         true,
	OpNin:        true,
	OpLike:       true,
	OpILike:      true,
	OpContains:   true,
	OpStartsWith: true,
	OpEndsWith:   true,
	OpExists:     true,
	OpIsNull:     true,
}

// IsValidOperator reports whether op is in the allowlist.
func IsValidOperator(op Operator) bool {
	return ValidOperators[op]
}

// IsComparisonOperator reports whether op is a leaf comparison (not logical).
func IsComparisonOperator(op Operator) bool {
	return comparisonOperators[op]
}

// IsLogicalOperator reports whether op is a logical combinator.
func IsLogicalOperator(op Operator) bool {
	return logicalOperators[op]
}
