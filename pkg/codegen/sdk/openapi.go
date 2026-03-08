// Package sdk provides OpenAPI 3.0.3 spec generation from registered schemas.
package sdk

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/digitally-rendered/stellar-drive/internal/stringutil"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// OpenAPISpec is the top-level OpenAPI 3.1 document.
type OpenAPISpec struct {
	OpenAPI    string                     `json:"openapi"`
	Info       OpenAPIInfo                `json:"info"`
	Paths      map[string]OpenAPIPathItem `json:"paths"`
	Components OpenAPIComponents          `json:"components"`
}

// OpenAPIInfo holds metadata about the API.
type OpenAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// OpenAPIPathItem maps HTTP methods to operation objects.
type OpenAPIPathItem map[string]*OpenAPIOperation

// OpenAPIOperation describes a single HTTP operation.
type OpenAPIOperation struct {
	Summary     string                     `json:"summary"`
	OperationID string                     `json:"operationId"`
	Tags        []string                   `json:"tags"`
	Parameters  []OpenAPIParameter         `json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]OpenAPIResponse `json:"responses"`
}

// OpenAPIParameter describes a path, query, or header parameter.
type OpenAPIParameter struct {
	Name     string        `json:"name"`
	In       string        `json:"in"`
	Required bool          `json:"required"`
	Schema   OpenAPISchema `json:"schema"`
}

// OpenAPIRequestBody describes the body of a mutating request.
type OpenAPIRequestBody struct {
	Required bool                        `json:"required"`
	Content  map[string]OpenAPIMediaType `json:"content"`
}

// OpenAPIMediaType wraps a schema for a specific media type.
type OpenAPIMediaType struct {
	Schema OpenAPISchema `json:"schema"`
}

// OpenAPIResponse describes a single HTTP response.
type OpenAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]OpenAPIMediaType `json:"content,omitempty"`
}

// OpenAPIComponents holds reusable schema definitions.
type OpenAPIComponents struct {
	Schemas map[string]OpenAPISchema `json:"schemas"`
}

// OpenAPISchema is a JSON Schema / OpenAPI schema object.
type OpenAPISchema struct {
	Type                 string                   `json:"type,omitempty"`
	Format               string                   `json:"format,omitempty"`
	Description          string                   `json:"description,omitempty"`
	Properties           map[string]OpenAPISchema `json:"properties,omitempty"`
	Required             []string                 `json:"required,omitempty"`
	Items                *OpenAPISchema           `json:"items,omitempty"`
	Enum                 []any                    `json:"enum,omitempty"`
	Ref                  string                   `json:"$ref,omitempty"`
	AdditionalProperties *OpenAPISchema           `json:"additionalProperties,omitempty"`
}

// GenerateOpenAPI builds an OpenAPI 3.0.3 spec from the latest version of all
// schemas registered in r. title is the API title; apiVersion is the spec
// version string (e.g. "1.0.0").
func GenerateOpenAPI(r *schema.Registry, title, apiVersion string) (*OpenAPISpec, error) {
	spec := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:   title,
			Version: apiVersion,
		},
		Paths:      make(map[string]OpenAPIPathItem),
		Components: OpenAPIComponents{Schemas: make(map[string]OpenAPISchema)},
	}

	for _, def := range r.List() {
		if err := addSchemaToSpec(spec, def); err != nil {
			return nil, fmt.Errorf("openapi: schema %q: %w", def.Name, err)
		}
	}
	return spec, nil
}

// GenerateVersionedOpenAPI builds an OpenAPI 3.0.3 spec from ALL registered
// schema versions.
//
// Component naming convention:
//   - Non-latest version "1.0.0" of "pet" → PetV1_0_0, PetV1_0_0Create, …
//   - Latest version also gets unversioned aliases: Pet, PetCreate, …
//
// Path naming convention:
//   - Latest version gets standard paths: /pets, /pets/{entityId}
//   - Non-latest gets versioned paths:    /pets@1.0.0, /pets@1.0.0/{entityId}
//
// All operations include an optional X-Schema-Version request header parameter.
func GenerateVersionedOpenAPI(r *schema.Registry, title, apiVersion string) (*OpenAPISpec, error) {
	spec := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:   title,
			Version: apiVersion,
		},
		Paths:      make(map[string]OpenAPIPathItem),
		Components: OpenAPIComponents{Schemas: make(map[string]OpenAPISchema)},
	}

	// Group all definitions by name so we can identify the latest per name.
	// r.ListAll() returns results sorted by name then version.
	allDefs := r.ListAll()
	byName := make(map[string][]*schema.SchemaDefinition)
	for _, def := range allDefs {
		byName[def.Name] = append(byName[def.Name], def)
	}

	// Collect names in deterministic order.
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	// Determine the latest version per name using the registry's own ordering.
	latestDefs := r.List() // one entry per name, already the latest version
	latestVersion := make(map[string]string, len(latestDefs))
	for _, def := range latestDefs {
		latestVersion[def.Name] = def.Version
	}

	for _, name := range names {
		defs := byName[name]
		latest := latestVersion[name]

		for _, def := range defs {
			isLatest := def.Version == latest
			if err := addVersionedSchemaToSpec(spec, def, isLatest); err != nil {
				return nil, fmt.Errorf("openapi versioned: schema %q v%s: %w", def.Name, def.Version, err)
			}
		}
	}

	return spec, nil
}

// versionSuffix converts a semver string such as "1.0.0" into "V1_0_0".
func versionSuffix(version string) string {
	// Strip leading "v" if present.
	v := strings.TrimPrefix(version, "v")
	return "V" + strings.ReplaceAll(v, ".", "_")
}

// addVersionedSchemaToSpec adds component schemas and paths for one specific
// version of a schema definition. When isLatest is true, unversioned aliases
// and standard (undecorated) paths are also registered.
func addVersionedSchemaToSpec(spec *OpenAPISpec, def *schema.SchemaDefinition, isLatest bool) error {
	typePrefix := stringutil.ToPascalCase(def.Name)
	vSuffix := versionSuffix(def.Version)
	versionedPrefix := typePrefix + vSuffix // e.g. PetV1_0_0

	basePath := "/" + strings.ToLower(stringutil.Pluralize(def.Name))
	tag := typePrefix

	// --- Component schemas (versioned) ---
	spec.Components.Schemas[versionedPrefix] = buildDocumentSchema(def)
	spec.Components.Schemas[versionedPrefix+"Create"] = buildCreateSchema(def)
	spec.Components.Schemas[versionedPrefix+"Update"] = buildUpdateSchema(def)
	spec.Components.Schemas[versionedPrefix+"ListResult"] = buildListResultSchema(versionedPrefix)

	// Latest version also gets unversioned aliases pointing at the versioned schemas.
	if isLatest {
		spec.Components.Schemas[typePrefix] = OpenAPISchema{Ref: "#/components/schemas/" + versionedPrefix}
		spec.Components.Schemas[typePrefix+"Create"] = OpenAPISchema{Ref: "#/components/schemas/" + versionedPrefix + "Create"}
		spec.Components.Schemas[typePrefix+"Update"] = OpenAPISchema{Ref: "#/components/schemas/" + versionedPrefix + "Update"}
		spec.Components.Schemas[typePrefix+"ListResult"] = OpenAPISchema{Ref: "#/components/schemas/" + versionedPrefix + "ListResult"}
	}

	schemaVersionHeader := OpenAPIParameter{
		Name:     "X-Schema-Version",
		In:       "header",
		Required: false,
		Schema:   OpenAPISchema{Type: "string"},
	}

	entityIDParam := OpenAPIParameter{
		Name:     "entityId",
		In:       "path",
		Required: true,
		Schema:   OpenAPISchema{Type: "string"},
	}

	// --- Paths ---
	// Latest version uses the standard path. Non-latest uses a versioned path.
	collectionPath := basePath
	entityPath := basePath + "/{entityId}"
	operationSuffix := "" // empty for latest

	if !isLatest {
		vTag := "@" + def.Version
		collectionPath = basePath + vTag
		entityPath = basePath + vTag + "/{entityId}"
		operationSuffix = vSuffix // e.g. V1_0_0
	}

	collectionItem := OpenAPIPathItem{
		"post": {
			Summary:     "Create " + def.Name,
			OperationID: "create" + typePrefix + operationSuffix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{schemaVersionHeader},
			RequestBody: jsonRequestBody(versionedPrefix + "Create"),
			Responses: map[string]OpenAPIResponse{
				"201": jsonResponse("Created "+versionedPrefix, versionedPrefix),
				"400": errorResponse("Bad request"),
			},
		},
		"get": {
			Summary:     "List " + def.Name,
			OperationID: "list" + typePrefix + operationSuffix,
			Tags:        []string{tag},
			Parameters:  append(paginationParameters(), schemaVersionHeader),
			Responses: map[string]OpenAPIResponse{
				"200": jsonResponse("Paginated "+versionedPrefix+" list", versionedPrefix+"ListResult"),
			},
		},
	}
	spec.Paths[collectionPath] = collectionItem

	entityItem := OpenAPIPathItem{
		"get": {
			Summary:     "Get " + def.Name + " by ID",
			OperationID: "get" + typePrefix + operationSuffix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{entityIDParam, schemaVersionHeader},
			Responses: map[string]OpenAPIResponse{
				"200": jsonResponse(versionedPrefix+" document", versionedPrefix),
				"404": errorResponse("Not found"),
			},
		},
		"patch": {
			Summary:     "Update " + def.Name,
			OperationID: "update" + typePrefix + operationSuffix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{entityIDParam, schemaVersionHeader},
			RequestBody: jsonRequestBody(versionedPrefix + "Update"),
			Responses: map[string]OpenAPIResponse{
				"200": jsonResponse("Updated "+versionedPrefix, versionedPrefix),
				"404": errorResponse("Not found"),
			},
		},
		"delete": {
			Summary:     "Delete " + def.Name,
			OperationID: "delete" + typePrefix + operationSuffix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{entityIDParam, schemaVersionHeader},
			Responses: map[string]OpenAPIResponse{
				"204": {Description: "No content"},
				"404": errorResponse("Not found"),
			},
		},
	}
	spec.Paths[entityPath] = entityItem

	return nil
}

// MarshalJSON returns the JSON bytes for the OpenAPI spec.
func MarshalJSON(spec *OpenAPISpec) ([]byte, error) {
	return json.MarshalIndent(spec, "", "  ")
}

// addSchemaToSpec adds component schemas and paths for def into spec.
func addSchemaToSpec(spec *OpenAPISpec, def *schema.SchemaDefinition) error {
	typePrefix := stringutil.ToPascalCase(def.Name)
	basePath := "/" + strings.ToLower(stringutil.Pluralize(def.Name))
	tag := typePrefix

	// Component schemas.
	spec.Components.Schemas[typePrefix] = buildDocumentSchema(def)
	spec.Components.Schemas[typePrefix+"Create"] = buildCreateSchema(def)
	spec.Components.Schemas[typePrefix+"Update"] = buildUpdateSchema(def)
	spec.Components.Schemas[typePrefix+"ListResult"] = buildListResultSchema(typePrefix)

	// Collection path: GET list + POST create.
	collectionItem := OpenAPIPathItem{
		"post": {
			Summary:     "Create " + def.Name,
			OperationID: "create" + typePrefix,
			Tags:        []string{tag},
			RequestBody: jsonRequestBody(typePrefix + "Create"),
			Responses: map[string]OpenAPIResponse{
				"201": jsonResponse("Created "+typePrefix, typePrefix),
				"400": errorResponse("Bad request"),
			},
		},
		"get": {
			Summary:     "List " + def.Name,
			OperationID: "list" + typePrefix,
			Tags:        []string{tag},
			Parameters:  paginationParameters(),
			Responses: map[string]OpenAPIResponse{
				"200": jsonResponse("Paginated "+typePrefix+" list", typePrefix+"ListResult"),
			},
		},
	}
	spec.Paths[basePath] = collectionItem

	// Entity path: GET / PATCH / DELETE.
	entityPath := basePath + "/{entityId}"
	entityIDParam := OpenAPIParameter{
		Name:     "entityId",
		In:       "path",
		Required: true,
		Schema:   OpenAPISchema{Type: "string"},
	}
	entityItem := OpenAPIPathItem{
		"get": {
			Summary:     "Get " + def.Name + " by ID",
			OperationID: "get" + typePrefix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{entityIDParam},
			Responses: map[string]OpenAPIResponse{
				"200": jsonResponse(typePrefix+" document", typePrefix),
				"404": errorResponse("Not found"),
			},
		},
		"patch": {
			Summary:     "Update " + def.Name,
			OperationID: "update" + typePrefix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{entityIDParam},
			RequestBody: jsonRequestBody(typePrefix + "Update"),
			Responses: map[string]OpenAPIResponse{
				"200": jsonResponse("Updated "+typePrefix, typePrefix),
				"404": errorResponse("Not found"),
			},
		},
		"delete": {
			Summary:     "Delete " + def.Name,
			OperationID: "delete" + typePrefix,
			Tags:        []string{tag},
			Parameters:  []OpenAPIParameter{entityIDParam},
			Responses: map[string]OpenAPIResponse{
				"204": {Description: "No content"},
				"404": errorResponse("Not found"),
			},
		},
	}
	spec.Paths[entityPath] = entityItem

	return nil
}

// buildDocumentSchema builds the OpenAPI schema for the Document variant.
func buildDocumentSchema(def *schema.SchemaDefinition) OpenAPISchema {
	props := map[string]OpenAPISchema{
		"id":             {Type: "string"},
		"entity_id":      {Type: "string"},
		"record_version": {Type: "integer", Format: "int64"},
		"schema_version": {Type: "string"},
		"schema_name":    {Type: "string"},
		"etag":           {Type: "string"},
		"created_at":     {Type: "string", Format: "date-time"},
		"updated_at":     {Type: "string", Format: "date-time"},
		"deleted_at":     {Type: "string", Format: "date-time"},
		"created_by":     {Type: "string"},
		"updated_by":     {Type: "string"},
		"deleted_by":     {Type: "string"},
	}
	required := []string{"id", "entity_id", "record_version", "schema_version", "schema_name", "etag", "created_at", "updated_at"}

	for _, f := range def.Fields {
		props[f.Name] = fieldToOpenAPISchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}

	return OpenAPISchema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

// buildCreateSchema builds the OpenAPI schema for the Create variant.
func buildCreateSchema(def *schema.SchemaDefinition) OpenAPISchema {
	props := make(map[string]OpenAPISchema, len(def.Fields))
	var required []string
	for _, f := range def.Fields {
		props[f.Name] = fieldToOpenAPISchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	s := OpenAPISchema{Type: "object", Properties: props}
	if len(required) > 0 {
		s.Required = required
	}
	return s
}

// buildUpdateSchema builds the OpenAPI schema for the Update variant (all optional).
func buildUpdateSchema(def *schema.SchemaDefinition) OpenAPISchema {
	props := make(map[string]OpenAPISchema, len(def.Fields))
	for _, f := range def.Fields {
		props[f.Name] = fieldToOpenAPISchema(f)
	}
	return OpenAPISchema{Type: "object", Properties: props}
}

// buildListResultSchema builds the OpenAPI schema for a paginated list response.
func buildListResultSchema(typePrefix string) OpenAPISchema {
	return OpenAPISchema{
		Type: "object",
		Properties: map[string]OpenAPISchema{
			"items": {
				Type:  "array",
				Items: &OpenAPISchema{Ref: "#/components/schemas/" + typePrefix},
			},
			"total":    {Type: "integer", Format: "int64"},
			"has_more": {Type: "boolean"},
			"cursor":   {Type: "string"},
		},
		Required: []string{"items", "total", "has_more"},
	}
}

// fieldToOpenAPISchema converts a FieldDefinition to an OpenAPISchema.
func fieldToOpenAPISchema(f schema.FieldDefinition) OpenAPISchema {
	if f.Ref != "" {
		return OpenAPISchema{Ref: f.Ref}
	}
	s := OpenAPISchema{}
	if len(f.Enum) > 0 {
		s.Type = "string"
		s.Enum = f.Enum
		return s
	}
	switch f.JSONType {
	case "string":
		s.Type = "string"
		if f.Format != "" {
			s.Format = f.Format
		}
	case "number":
		s.Type = "number"
		s.Format = "double"
	case "integer":
		s.Type = "integer"
		s.Format = "int64"
	case "boolean":
		s.Type = "boolean"
	case "array":
		s.Type = "array"
		if f.Items != nil {
			itemSchema := fieldToOpenAPISchema(*f.Items)
			s.Items = &itemSchema
		} else {
			s.Items = &OpenAPISchema{Type: "string"}
		}
	case "object":
		s.Type = "object"
		if len(f.Properties) > 0 {
			s.Properties = make(map[string]OpenAPISchema, len(f.Properties))
			for _, p := range f.Properties {
				s.Properties[p.Name] = fieldToOpenAPISchema(p)
			}
		} else {
			s.AdditionalProperties = &OpenAPISchema{Type: "string"}
		}
	default:
		s.Type = "string"
	}
	return s
}

// jsonRequestBody constructs a request body pointing to a component schema ref.
func jsonRequestBody(schemaName string) *OpenAPIRequestBody {
	return &OpenAPIRequestBody{
		Required: true,
		Content: map[string]OpenAPIMediaType{
			"application/json": {
				Schema: OpenAPISchema{Ref: "#/components/schemas/" + schemaName},
			},
		},
	}
}

// jsonResponse constructs a response with a JSON body pointing to a component schema.
func jsonResponse(description, schemaName string) OpenAPIResponse {
	return OpenAPIResponse{
		Description: description,
		Content: map[string]OpenAPIMediaType{
			"application/json": {
				Schema: OpenAPISchema{Ref: "#/components/schemas/" + schemaName},
			},
		},
	}
}

// errorResponse constructs a plain error response without a body schema.
func errorResponse(description string) OpenAPIResponse {
	return OpenAPIResponse{
		Description: description,
		Content: map[string]OpenAPIMediaType{
			"application/json": {
				Schema: OpenAPISchema{
					Type: "object",
					Properties: map[string]OpenAPISchema{
						"error": {Type: "string"},
					},
				},
			},
		},
	}
}

// paginationParameters returns the standard pagination query parameters.
func paginationParameters() []OpenAPIParameter {
	return []OpenAPIParameter{
		{Name: "limit", In: "query", Schema: OpenAPISchema{Type: "integer"}},
		{Name: "offset", In: "query", Schema: OpenAPISchema{Type: "integer"}},
		{Name: "cursor", In: "query", Schema: OpenAPISchema{Type: "string"}},
	}
}
