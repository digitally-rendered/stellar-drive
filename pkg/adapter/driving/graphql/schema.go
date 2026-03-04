// Package graphql implements the GraphQL driving adapter for stellar-drive.
// It builds a complete GraphQL schema at runtime from the registered schema
// definitions, and wires each type to generic CRUD resolvers backed by the
// port.Service interface.
package graphql

import (
	"fmt"
	"strings"

	gql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/digitally-rendered/stellar-drive/pkg/core/container"
	schemapkg "github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// jsonScalar is a custom scalar type that passes any JSON-serialisable value
// through unchanged. It is used for "object" fields without defined properties
// and for the "input" argument on create/update mutations.
var jsonScalar = gql.NewScalar(gql.ScalarConfig{
	Name:        "JSON",
	Description: "An arbitrary JSON value (object, array, scalar, or null).",
	Serialize:  func(value any) any { return value },
	ParseValue: func(value any) any { return value },
	ParseLiteral: func(valueAST ast.Value) any {
		return valueAST.GetValue()
	},
})

// deleteResultType is the shared return type for all delete mutations.
var deleteResultType = gql.NewObject(gql.ObjectConfig{
	Name:        "DeleteResult",
	Description: "Result of a soft-delete mutation.",
	Fields: gql.Fields{
		"deleted": &gql.Field{
			Type:        gql.NewNonNull(gql.Boolean),
			Description: "Always true on success.",
		},
		"entityId": &gql.Field{
			Type:        gql.NewNonNull(gql.ID),
			Description: "The entity ID of the deleted record.",
		},
	},
})

// auditFields returns the common audit gql.Fields that are appended to every
// generated object type. They map directly to model.Document system fields.
func auditFields() gql.Fields {
	return gql.Fields{
		"id": &gql.Field{
			Type:        gql.NewNonNull(gql.ID),
			Description: "Internal storage identifier (MongoDB _id).",
		},
		"entity_id": &gql.Field{
			Type:        gql.NewNonNull(gql.ID),
			Description: "Stable public entity identifier.",
		},
		"record_version": &gql.Field{
			Type:        gql.NewNonNull(gql.Int),
			Description: "Monotonically increasing version for optimistic locking.",
		},
		"schema_version": &gql.Field{
			Type:        gql.NewNonNull(gql.String),
			Description: "JSON Schema version in use when this record was written.",
		},
		"created_at": &gql.Field{
			Type:        gql.NewNonNull(gql.DateTime),
			Description: "UTC timestamp of the record's creation.",
		},
		"updated_at": &gql.Field{
			Type:        gql.NewNonNull(gql.DateTime),
			Description: "UTC timestamp of the last update.",
		},
		"deleted_at": &gql.Field{
			Type:        gql.DateTime,
			Description: "UTC timestamp of soft-deletion; null for live records.",
		},
		"etag": &gql.Field{
			Type:        gql.NewNonNull(gql.String),
			Description: "HTTP ETag for conditional requests.",
		},
	}
}

// fieldDefToGQLType maps a FieldDefinition to the most appropriate GraphQL
// output type. Nested objects and arrays are handled recursively.
// typeRegistry is used to avoid creating duplicate named types for nested
// objects (which GraphQL forbids).
func fieldDefToGQLType(fd schemapkg.FieldDefinition, typeRegistry map[string]gql.Output) gql.Output {
	switch fd.JSONType {
	case "string":
		if fd.Format == "date-time" {
			return gql.DateTime
		}
		return gql.String

	case "integer":
		return gql.Int

	case "number":
		return gql.Float

	case "boolean":
		return gql.Boolean

	case "array":
		if fd.Items == nil {
			return gql.NewList(jsonScalar)
		}
		itemType := fieldDefToGQLType(*fd.Items, typeRegistry)
		return gql.NewList(itemType)

	case "object":
		if len(fd.Properties) == 0 {
			return jsonScalar
		}
		// Build a named nested object type.
		typeName := "NestedObject_" + sanitizeName(fd.Name)
		if existing, ok := typeRegistry[typeName]; ok {
			return existing
		}
		fields := gql.Fields{}
		for _, prop := range fd.Properties {
			propType := fieldDefToGQLType(prop, typeRegistry)
			fields[prop.Name] = &gql.Field{Type: propType}
		}
		obj := gql.NewObject(gql.ObjectConfig{
			Name:   typeName,
			Fields: fields,
		})
		typeRegistry[typeName] = obj
		return obj

	default:
		return jsonScalar
	}
}

// sanitizeName strips characters that are invalid in GraphQL type identifiers.
func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}

// buildObjectType constructs the GraphQL Object type for a schema definition.
// Audit fields are appended after the user-defined fields.
func buildObjectType(def *schemapkg.SchemaDefinition, typeRegistry map[string]gql.Output) *gql.Object {
	fields := auditFields()

	for _, fd := range def.Fields {
		gqlType := fieldDefToGQLType(fd, typeRegistry)
		if fd.Required {
			gqlType = gql.NewNonNull(gqlType)
		}
		fields[fd.Name] = &gql.Field{
			Type:        gqlType,
			Description: fmt.Sprintf("Field %q from schema %q.", fd.Name, def.Name),
		}
	}

	return gql.NewObject(gql.ObjectConfig{
		Name:        toPascalCase(def.Name),
		Description: def.Description,
		Fields:      fields,
	})
}

// listResultType builds a simple list result type that carries items, total,
// and hasMore — the flat form returned by makeListResolver.
func listResultType(name string, nodeType *gql.Object) *gql.Object {
	return gql.NewObject(gql.ObjectConfig{
		Name:        name + "ListResult",
		Description: "Paginated list result for " + name + ".",
		Fields: gql.Fields{
			"items": &gql.Field{
				Type:        gql.NewList(gql.NewNonNull(nodeType)),
				Description: "Records on the current page.",
			},
			"total": &gql.Field{
				Type:        gql.NewNonNull(gql.Int),
				Description: "Total matching records (before pagination).",
			},
			"hasMore": &gql.Field{
				Type:        gql.NewNonNull(gql.Boolean),
				Description: "True when more pages exist.",
			},
		},
	})
}

// BuildSchema constructs a complete graphql.Schema from every definition in
// registry, wiring each type to CRUD resolvers via the DI container.
//
// For each registered schema the function creates:
//   - A GraphQL Object type with all user-defined fields plus audit fields.
//   - A Relay connection type for paginated listing.
//   - Query fields:    get{Name}(entityId: ID!)  and  list{Name}s(...)
//   - Mutation fields: create{Name}(input: JSON!), update{Name}(entityId: ID!, input: JSON!),
//     delete{Name}(entityId: ID!)
func BuildSchema(registry *schemapkg.Registry, ctr *container.Container) (gql.Schema, error) {
	defs := registry.List()
	if len(defs) == 0 {
		// An empty schema is valid but we still need at least a query root.
		return buildEmptySchema()
	}

	queryFields := gql.Fields{}
	mutationFields := gql.Fields{}

	// typeRegistry prevents duplicate nested object type names within a single
	// BuildSchema call.
	typeRegistry := make(map[string]gql.Output)

	for _, def := range defs {
		svc := ctr.ResolveService(def.Name)
		if svc == nil {
			// Skip schemas with no service wired up; they cannot be queried.
			continue
		}

		objectType := buildObjectType(def, typeRegistry)
		connType := ConnectionType(toPascalCase(def.Name), objectType)
		listType := listResultType(toPascalCase(def.Name), objectType)

		pascal := toPascalCase(def.Name)
		listField := "list" + pascal + "s"

		// ---- Query fields ------------------------------------------------

		queryFields["get"+pascal] = &gql.Field{
			Type:        objectType,
			Description: "Fetch a single " + def.Name + " record by its entity ID.",
			Args: gql.FieldConfigArgument{
				"entityId": &gql.ArgumentConfig{
					Type:        gql.NewNonNull(gql.ID),
					Description: "Stable entity identifier.",
				},
			},
			Resolve: makeGetResolver(def.Name, svc),
		}

		_ = connType // connection type is available for future subscription / relay use

		queryFields[listField] = &gql.Field{
			Type:        listType,
			Description: "List " + def.Name + " records with optional filtering and pagination.",
			Args: gql.FieldConfigArgument{
				"where": &gql.ArgumentConfig{
					Type:        gql.String,
					Description: "JSON filter expression in stellar-drive query DSL.",
				},
				"sort": &gql.ArgumentConfig{
					Type:        gql.String,
					Description: `Comma-separated sort expression, e.g. "+name,-created_at".`,
				},
				"limit": &gql.ArgumentConfig{
					Type:         gql.Int,
					Description:  "Maximum number of records to return (default 20, max 100).",
					DefaultValue: 20,
				},
				"offset": &gql.ArgumentConfig{
					Type:         gql.Int,
					Description:  "Zero-based record offset for pagination.",
					DefaultValue: 0,
				},
			},
			Resolve: makeListResolver(def.Name, svc),
		}

		// ---- Mutation fields ---------------------------------------------

		mutationFields["create"+pascal] = &gql.Field{
			Type:        objectType,
			Description: "Create a new " + def.Name + " record.",
			Args: gql.FieldConfigArgument{
				"input": &gql.ArgumentConfig{
					Type:        gql.NewNonNull(jsonScalar),
					Description: "Field values for the new record.",
				},
			},
			Resolve: makeCreateResolver(def.Name, svc),
		}

		mutationFields["update"+pascal] = &gql.Field{
			Type:        objectType,
			Description: "Partially update an existing " + def.Name + " record.",
			Args: gql.FieldConfigArgument{
				"entityId": &gql.ArgumentConfig{
					Type:        gql.NewNonNull(gql.ID),
					Description: "Entity ID of the record to update.",
				},
				"input": &gql.ArgumentConfig{
					Type:        gql.NewNonNull(jsonScalar),
					Description: "Partial field values to apply.",
				},
			},
			Resolve: makeUpdateResolver(def.Name, svc),
		}

		mutationFields["delete"+pascal] = &gql.Field{
			Type:        deleteResultType,
			Description: "Soft-delete a " + def.Name + " record.",
			Args: gql.FieldConfigArgument{
				"entityId": &gql.ArgumentConfig{
					Type:        gql.NewNonNull(gql.ID),
					Description: "Entity ID of the record to delete.",
				},
			},
			Resolve: makeDeleteResolver(def.Name, svc),
		}
	}

	if len(queryFields) == 0 {
		return buildEmptySchema()
	}

	queryType := gql.NewObject(gql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	schemaConfig := gql.SchemaConfig{
		Query: queryType,
	}

	if len(mutationFields) > 0 {
		schemaConfig.Mutation = gql.NewObject(gql.ObjectConfig{
			Name:   "Mutation",
			Fields: mutationFields,
		})
	}

	return gql.NewSchema(schemaConfig)
}

// buildEmptySchema returns a minimal valid GraphQL schema with a single
// placeholder query field. It is used when no schemas are registered.
func buildEmptySchema() (gql.Schema, error) {
	return gql.NewSchema(gql.SchemaConfig{
		Query: gql.NewObject(gql.ObjectConfig{
			Name: "Query",
			Fields: gql.Fields{
				"_empty": &gql.Field{
					Type:        gql.String,
					Description: "Placeholder field. No schemas are currently registered.",
					Resolve: func(p gql.ResolveParams) (any, error) {
						return "no schemas registered", nil
					},
				},
			},
		}),
	})
}

// toPascalCase converts a snake_case or already-capitalised name to PascalCase
// without importing the internal stringutil package.
func toPascalCase(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = toUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
