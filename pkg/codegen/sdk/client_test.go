package sdk_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/codegen/sdk"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// petSchema returns a schema.Registry pre-loaded with a "pet" schema that
// exercises string, integer, and boolean field types.
func petRegistry(t *testing.T) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	_, err := reg.Register(&schema.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Storage: "mongo",
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"name", "species"},
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"species":     map[string]any{"type": "string"},
				"age":         map[string]any{"type": "integer"},
				"weight":      map[string]any{"type": "number"},
				"vaccinated":  map[string]any{"type": "boolean"},
				"description": map[string]any{"type": "string"},
			},
		},
		Active: true,
	})
	require.NoError(t, err)
	return reg
}

// multiRegistry returns a registry with "order" and "product" schemas to
// exercise multi-schema generation.
func multiRegistry(t *testing.T) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()

	schemas := []struct {
		name    string
		env     *schema.SchemaEnvelope
	}{
		{
			name: "order",
			env: &schema.SchemaEnvelope{
				Name:    "order",
				Version: "1.0.0",
				Storage: "mongo",
				Schema: map[string]any{
					"type":     "object",
					"required": []any{"customer_id", "amount"},
					"properties": map[string]any{
						"customer_id": map[string]any{"type": "string"},
						"amount":      map[string]any{"type": "number"},
						"quantity":    map[string]any{"type": "integer"},
						"is_paid":     map[string]any{"type": "boolean"},
					},
				},
				Active: true,
			},
		},
		{
			name: "product",
			env: &schema.SchemaEnvelope{
				Name:    "product",
				Version: "1.0.0",
				Storage: "mongo",
				Schema: map[string]any{
					"type":     "object",
					"required": []any{"sku"},
					"properties": map[string]any{
						"sku":   map[string]any{"type": "string"},
						"price": map[string]any{"type": "number"},
						"stock": map[string]any{"type": "integer"},
					},
				},
				Active: true,
			},
		},
	}

	for _, s := range schemas {
		_, err := reg.Register(s.env)
		require.NoError(t, err, "register schema %q", s.name)
	}
	return reg
}

// parseable asserts that src is valid Go source code.
func parseable(t *testing.T, filename, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, filename, src, parser.AllErrors)
	require.NoError(t, err, "generated %s must be parseable Go:\n%s", filename, src)
}

// ---------------------------------------------------------------------------
// 1. Models — simple schema with string, integer, boolean fields
// ---------------------------------------------------------------------------

func TestClientGenerator_Models_StructsPresent(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models, ok := files["models.go"]
	require.True(t, ok, "models.go must be present in generated output")

	// All three variants must exist.
	assert.Contains(t, models, "type Pet struct", "Pet base struct must be present")
	assert.Contains(t, models, "type PetCreate struct", "PetCreate struct must be present")
	assert.Contains(t, models, "type PetUpdate struct", "PetUpdate struct must be present")
}

func TestClientGenerator_Models_FieldTypes(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	// String field.
	assert.Contains(t, models, "Name string", "string field must map to Go string")
	// Integer field.
	assert.Contains(t, models, "Age int64", "integer field must map to Go int64")
	// Number field.
	assert.Contains(t, models, "Weight float64", "number field must map to Go float64")
	// Boolean field.
	assert.Contains(t, models, "Vaccinated bool", "boolean field must map to Go bool")
}

func TestClientGenerator_Models_UpdateVariantPointers(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	updateIdx := strings.Index(models, "type PetUpdate struct")
	require.True(t, updateIdx >= 0, "PetUpdate struct must be present")

	updateBlock := models[updateIdx:]
	endIdx := strings.Index(updateBlock, "\n}")
	if endIdx > 0 {
		updateBlock = updateBlock[:endIdx]
	}

	// Scalar types must be pointer-wrapped in the Update variant.
	assert.Contains(t, updateBlock, "*int64", "integer field must be *int64 in Update struct")
	assert.Contains(t, updateBlock, "*float64", "number field must be *float64 in Update struct")
	assert.Contains(t, updateBlock, "*bool", "boolean field must be *bool in Update struct")
}

func TestClientGenerator_Models_RequiredFieldTag(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	createIdx := strings.Index(models, "type PetCreate struct")
	require.True(t, createIdx >= 0, "PetCreate must be present")
	createBlock := models[createIdx:]

	// "name" is required — its json tag must not carry omitempty.
	nameTagIdx := strings.Index(createBlock, `"name"`)
	require.True(t, nameTagIdx >= 0, "name json tag must be present in PetCreate")
	tagRegion := createBlock[nameTagIdx : nameTagIdx+40]
	assert.NotContains(t, tagRegion, "omitempty", "required field must not have omitempty in Create")
}

func TestClientGenerator_Models_OptionalFieldTag(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	createIdx := strings.Index(models, "type PetCreate struct")
	require.True(t, createIdx >= 0, "PetCreate must be present")
	createBlock := models[createIdx:]

	// "age" is optional — its json tag must carry omitempty.
	ageTagIdx := strings.Index(createBlock, `"age`)
	require.True(t, ageTagIdx >= 0, "age json tag must be present in PetCreate")
	tagRegion := createBlock[ageTagIdx : ageTagIdx+50]
	assert.Contains(t, tagRegion, "omitempty", "optional field must have omitempty in Create")
}

func TestClientGenerator_Models_GenericHelpers(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	assert.Contains(t, models, "ResponseEnvelope[T any]", "ResponseEnvelope generic must be present")
	assert.Contains(t, models, "ListResult[T any]", "ListResult generic must be present")
	assert.Contains(t, models, "Document[T any]", "Document generic must be present")
	assert.Contains(t, models, "ListOptions", "ListOptions struct must be present")
}

func TestClientGenerator_Models_DocumentAuditFields(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	assert.Contains(t, models, "ID ", "Document must have ID field")
	assert.Contains(t, models, "EntityID ", "Document must have EntityID field")
	assert.Contains(t, models, "RecordVersion ", "Document must have RecordVersion field")
	assert.Contains(t, models, "ETag ", "Document must have ETag field")
	assert.Contains(t, models, "CreatedAt ", "Document must have CreatedAt field")
	assert.Contains(t, models, "UpdatedAt ", "Document must have UpdatedAt field")
}

// ---------------------------------------------------------------------------
// 2. Client — correct method signatures
// ---------------------------------------------------------------------------

func TestClientGenerator_Client_MethodSignatures(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	tests := []struct {
		method  string
		present bool
	}{
		{"func (c *Client) CreatePet(ctx context.Context, input PetCreate)", true},
		{"func (c *Client) GetPet(ctx context.Context, id string)", true},
		{"func (c *Client) ListPets(ctx context.Context, opts ListOptions)", true},
		{"func (c *Client) UpdatePet(ctx context.Context, id string, input PetUpdate)", true},
		{"func (c *Client) DeletePet(ctx context.Context, id string)", true},
	}

	for _, tt := range tests {
		assert.Contains(t, client, tt.method, "generated client must include method signature: %s", tt.method)
	}
}

func TestClientGenerator_Client_ReturnTypes(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	assert.Contains(t, client, "(*Document[Pet], error)", "Create/Get/Update must return *Document[Pet]")
	assert.Contains(t, client, "(*ListResult[Document[Pet]], error)", "List must return *ListResult[Document[Pet]]")
}

func TestClientGenerator_Client_URLPaths(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	// Collection path for list/create.
	assert.Contains(t, client, `"/pets"`, "collection path /pets must be used for list and create")
	// Entity path for get/update/delete.
	assert.Contains(t, client, `"/pets"`, "entity base path /pets must be used for get/update/delete")
}

func TestClientGenerator_Client_ContentTypeHeader(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	assert.Contains(t, client, `"Content-Type", "application/json"`,
		"mutating requests must set Content-Type: application/json")
}

func TestClientGenerator_Client_HTTPMethods(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	assert.Contains(t, client, "http.MethodPost", "Create must use POST")
	assert.Contains(t, client, "http.MethodGet", "Get/List must use GET")
	assert.Contains(t, client, "http.MethodPatch", "Update must use PATCH")
	assert.Contains(t, client, "http.MethodDelete", "Delete must use DELETE")
}

func TestClientGenerator_Client_WithBaseURL(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir(),
		sdk.WithBaseURL("https://api.example.com"),
	)

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	assert.Contains(t, client, "https://api.example.com",
		"custom base URL must appear in generated NewClient default")
}

// ---------------------------------------------------------------------------
// 3. Imports — generated files include the right imports
// ---------------------------------------------------------------------------

func TestClientGenerator_Client_Imports(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	requiredImports := []string{
		`"bytes"`,
		`"context"`,
		`"encoding/json"`,
		`"fmt"`,
		`"io"`,
		`"net/http"`,
		`"net/url"`,
		`"strconv"`,
	}
	for _, imp := range requiredImports {
		assert.Contains(t, client, imp, "client.go must import %s", imp)
	}
}

func TestClientGenerator_Errors_Imports(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	errors := files["errors.go"]

	assert.Contains(t, errors, `"encoding/json"`, "errors.go must import encoding/json")
	assert.Contains(t, errors, `"fmt"`, "errors.go must import fmt")
}

func TestClientGenerator_Models_Imports(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	assert.Contains(t, models, `"time"`, "models.go must import time for audit fields")
}

// ---------------------------------------------------------------------------
// 4. Multiple schemas
// ---------------------------------------------------------------------------

func TestClientGenerator_MultipleSchemas_AllMethodsPresent(t *testing.T) {
	reg := multiRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]
	models := files["models.go"]

	// Order methods.
	assert.Contains(t, client, "func (c *Client) CreateOrder(", "CreateOrder must be present")
	assert.Contains(t, client, "func (c *Client) GetOrder(", "GetOrder must be present")
	assert.Contains(t, client, "func (c *Client) ListOrders(", "ListOrders must be present")
	assert.Contains(t, client, "func (c *Client) UpdateOrder(", "UpdateOrder must be present")
	assert.Contains(t, client, "func (c *Client) DeleteOrder(", "DeleteOrder must be present")

	// Product methods.
	assert.Contains(t, client, "func (c *Client) CreateProduct(", "CreateProduct must be present")
	assert.Contains(t, client, "func (c *Client) GetProduct(", "GetProduct must be present")
	assert.Contains(t, client, "func (c *Client) ListProducts(", "ListProducts must be present")
	assert.Contains(t, client, "func (c *Client) UpdateProduct(", "UpdateProduct must be present")
	assert.Contains(t, client, "func (c *Client) DeleteProduct(", "DeleteProduct must be present")

	// Both model sets.
	assert.Contains(t, models, "type Order struct", "Order struct must be present")
	assert.Contains(t, models, "type Product struct", "Product struct must be present")
}

func TestClientGenerator_MultipleSchemas_CorrectPaths(t *testing.T) {
	reg := multiRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	assert.Contains(t, client, `"/orders"`, "order path /orders must be used")
	assert.Contains(t, client, `"/products"`, "product path /products must be used")
}

func TestClientGenerator_MultipleSchemas_ModelsForEach(t *testing.T) {
	reg := multiRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	models := files["models.go"]

	for _, prefix := range []string{"Order", "Product"} {
		assert.Contains(t, models, "type "+prefix+" struct", "%s base struct must be present", prefix)
		assert.Contains(t, models, "type "+prefix+"Create struct", "%sCreate must be present", prefix)
		assert.Contains(t, models, "type "+prefix+"Update struct", "%sUpdate must be present", prefix)
	}
}

// ---------------------------------------------------------------------------
// 5. Generated code header and package declaration
// ---------------------------------------------------------------------------

func TestClientGenerator_GeneratedFileHeader(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	for name, content := range files {
		assert.Contains(t, content, "Code generated by stellar-drive",
			"%s must include generated file header", name)
		assert.Contains(t, content, "DO NOT EDIT",
			"%s must include DO NOT EDIT comment", name)
		assert.Contains(t, content, "package sdkclient",
			"%s must declare package sdkclient", name)
	}
}

// ---------------------------------------------------------------------------
// 6. Errors — APIError implements error correctly
// ---------------------------------------------------------------------------

func TestClientGenerator_Errors_APIErrorFields(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	errors := files["errors.go"]

	assert.Contains(t, errors, "type APIError struct", "APIError struct must be declared")
	assert.Contains(t, errors, "StatusCode int", "APIError must have StatusCode field")
	assert.Contains(t, errors, "Code ", "APIError must have Code field")
	assert.Contains(t, errors, "Message ", "APIError must have Message field")
	assert.Contains(t, errors, "Details []ErrorDetail", "APIError must have Details field")
	assert.Contains(t, errors, "func (e *APIError) Error() string", "APIError must implement error interface")
}

func TestClientGenerator_Errors_ErrorDetail(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	errors := files["errors.go"]

	assert.Contains(t, errors, "type ErrorDetail struct", "ErrorDetail struct must be declared")
	assert.Contains(t, errors, "Field ", "ErrorDetail must have Field")
	assert.Contains(t, errors, "Message ", "ErrorDetail must have Message")
	assert.Contains(t, errors, "Code ", "ErrorDetail must have Code")
}

// ---------------------------------------------------------------------------
// 7. Client struct and constructor
// ---------------------------------------------------------------------------

func TestClientGenerator_Client_StructAndConstructor(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	client := files["client.go"]

	assert.Contains(t, client, "type Client struct", "Client struct must be declared")
	assert.Contains(t, client, "func NewClient(", "NewClient constructor must be present")
	assert.Contains(t, client, "httpClient *http.Client", "Client must embed *http.Client")
	assert.Contains(t, client, "baseURL string", "Client must store baseURL")
}

// ---------------------------------------------------------------------------
// 8. Empty registry produces valid (no-schema) output
// ---------------------------------------------------------------------------

func TestClientGenerator_EmptyRegistry(t *testing.T) {
	reg := schema.NewRegistry()
	gen := sdk.NewClientGenerator(reg, t.TempDir())

	files, err := gen.Generate()
	require.NoError(t, err)

	require.Contains(t, files, "models.go")
	require.Contains(t, files, "client.go")
	require.Contains(t, files, "errors.go")

	// The shared helpers must still be present even with no schemas.
	assert.Contains(t, files["models.go"], "ResponseEnvelope", "shared types must be present with empty registry")
	assert.Contains(t, files["client.go"], "type Client struct", "Client struct must exist with empty registry")

	// parseable must not fail even on empty registry output.
	parseable(t, "models.go", files["models.go"])
	parseable(t, "errors.go", files["errors.go"])
}

// ---------------------------------------------------------------------------
// 9. WithModulePath option is accepted without panicking
// ---------------------------------------------------------------------------

func TestClientGenerator_WithModulePath(t *testing.T) {
	reg := petRegistry(t)
	gen := sdk.NewClientGenerator(reg, t.TempDir(),
		sdk.WithModulePath("github.com/acme/myapp"),
	)

	files, err := gen.Generate()
	require.NoError(t, err)
	require.NotEmpty(t, files)
}
