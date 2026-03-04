package query

// DefaultSort returns the default sort order: created_at descending.
// This ensures that the most recently created records appear first when no
// explicit sort is provided.
func DefaultSort() []SortField {
	return []SortField{
		{Field: "created_at", Direction: SortDesc},
	}
}
