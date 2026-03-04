package graphql

import (
	gql "github.com/graphql-go/graphql"
)

// PageInfoType is the shared Relay-style PageInfo GraphQL type.
// It is declared as a package-level variable so it can be reused across all
// connection types without creating duplicate type definitions in the schema.
var PageInfoType = gql.NewObject(gql.ObjectConfig{
	Name:        "PageInfo",
	Description: "Pagination metadata for a Relay-style connection.",
	Fields: gql.Fields{
		"hasNextPage": &gql.Field{
			Type:        gql.NewNonNull(gql.Boolean),
			Description: "True when there are more records after the current page.",
		},
		"hasPreviousPage": &gql.Field{
			Type:        gql.NewNonNull(gql.Boolean),
			Description: "True when there are more records before the current page.",
		},
		"startCursor": &gql.Field{
			Type:        gql.String,
			Description: "Opaque cursor pointing to the first edge in the current page.",
		},
		"endCursor": &gql.Field{
			Type:        gql.String,
			Description: "Opaque cursor pointing to the last edge in the current page.",
		},
		"total": &gql.Field{
			Type:        gql.Int,
			Description: "Total number of records matching the query (before pagination).",
		},
	},
})

// ConnectionType creates a Relay-style connection type for the given node type.
// The returned object has:
//
//   - edges: [{node: T, cursor: String}]
//   - pageInfo: PageInfo
//
// name should be the PascalCase schema name (e.g. "Product" produces
// "ProductConnection", "ProductEdge").
func ConnectionType(name string, nodeType *gql.Object) *gql.Object {
	edgeType := gql.NewObject(gql.ObjectConfig{
		Name:        name + "Edge",
		Description: "An edge in a " + name + " connection.",
		Fields: gql.Fields{
			"node": &gql.Field{
				Type:        nodeType,
				Description: "The " + name + " record at this edge.",
			},
			"cursor": &gql.Field{
				Type:        gql.NewNonNull(gql.String),
				Description: "Opaque pagination cursor for this edge.",
			},
		},
	})

	return gql.NewObject(gql.ObjectConfig{
		Name:        name + "Connection",
		Description: "Paginated list of " + name + " records.",
		Fields: gql.Fields{
			"edges": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(edgeType)),
				Description: "List of edges containing nodes and cursors.",
			},
			"pageInfo": &gql.Field{
				Type:        gql.NewNonNull(PageInfoType),
				Description: "Pagination metadata for this result set.",
			},
		},
	})
}
