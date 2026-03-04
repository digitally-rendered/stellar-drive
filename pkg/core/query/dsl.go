package query

// Query is the top-level structure representing a full query: filter, sort,
// pagination, field projection, and cursor state.
type Query struct {
	Filter  *FilterNode `json:"filter,omitempty"`
	Sort    []SortField `json:"sort,omitempty"`
	Limit   int         `json:"limit,omitempty"`
	Offset  int         `json:"offset,omitempty"`
	Cursor  string      `json:"cursor,omitempty"`
	Fields  []string    `json:"fields,omitempty"`
}

// Operator is a named query operator token.
type Operator string

const (
	OpEq         Operator = "_eq"
	OpNeq        Operator = "_neq"
	OpGt         Operator = "_gt"
	OpGte        Operator = "_gte"
	OpLt         Operator = "_lt"
	OpLte        Operator = "_lte"
	OpIn         Operator = "_in"
	OpNin        Operator = "_nin"
	OpLike       Operator = "_like"
	OpILike      Operator = "_ilike"
	OpContains   Operator = "_contains"
	OpStartsWith Operator = "_startswith"
	OpEndsWith   Operator = "_endswith"
	OpExists     Operator = "_exists"
	OpIsNull     Operator = "_is_null"
	OpAnd        Operator = "_and"
	OpOr         Operator = "_or"
	OpNot        Operator = "_not"
)

// FilterNode is a node in the filter expression tree.
// Leaf nodes have Field + Operator + Value.
// Logical nodes (_and, _or, _not) use Operator + Children.
type FilterNode struct {
	Field    string        `json:"field,omitempty"`
	Operator Operator      `json:"op,omitempty"`
	Value    any           `json:"value,omitempty"`
	Children []*FilterNode `json:"children,omitempty"`
}

// IsLogical reports whether this node is a logical combinator (_and, _or, _not).
func (f *FilterNode) IsLogical() bool {
	return f.Operator == OpAnd || f.Operator == OpOr || f.Operator == OpNot
}

// SortField specifies a sort key and direction.
type SortField struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

// SortDirection is the direction of a sort clause.
type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)
