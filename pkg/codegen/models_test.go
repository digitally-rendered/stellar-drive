package codegen_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/codegen"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// sampleSchema returns a minimal SchemaDefinition that exercises the common
// field types used by the code generator.
func sampleSchema() *schema.SchemaDefinition {
	reg := schema.NewRegistry()
	env := &schema.SchemaEnvelope{
		Name:    "order",
		Version: "1.0.0",
		Storage: "mongo",
		Schema: map[string]any{
			"type": "object",
			"required": []any{"customer_id", "amount"},
			"properties": map[string]any{
				"customer_id": map[string]any{
					"type": "string",
				},
				"amount": map[string]any{
					"type": "number",
				},
				"quantity": map[string]any{
					"type": "integer",
				},
				"is_paid": map[string]any{
					"type": "boolean",
				},
				"placed_at": map[string]any{
					"type":   "string",
					"format": "date-time",
				},
				"status": map[string]any{
					"type": "string",
					"enum": []any{"pending", "shipped", "delivered"},
				},
				"tags": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
				},
				"metadata": map[string]any{
					"type": "object",
				},
			},
		},
		Active: true,
	}
	def, err := reg.Register(env)
	if err != nil {
		panic("sampleSchema: " + err.Error())
	}
	return def
}

func TestGenerateModels_ValidGoSource(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "github.com/acme/app")
	require.NoError(t, err, "GenerateModels must not return an error")
	require.NotEmpty(t, src, "generated source must not be empty")

	// The generated source must parse as valid Go.
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "models.go", src, parser.AllErrors)
	require.NoError(t, parseErr, "generated source must be valid Go:\n%s", src)
}

func TestGenerateModels_PackageDeclaration(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	assert.Contains(t, src, "package order", "generated file must declare the correct package")
}

func TestGenerateModels_DocumentStruct(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	assert.Contains(t, src, "type OrderDocument struct", "Document variant struct must be present")
	assert.Contains(t, src, "ID ", "Document must include ID field")
	assert.Contains(t, src, "EntityID ", "Document must include EntityID field")
	assert.Contains(t, src, "RecordVersion ", "Document must include RecordVersion field")
	assert.Contains(t, src, "CreatedAt ", "Document must include CreatedAt field")
	assert.Contains(t, src, "ETag ", "Document must include ETag field")
}

func TestGenerateModels_CreateStruct(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	assert.Contains(t, src, "type OrderCreate struct", "Create variant struct must be present")
}

func TestGenerateModels_UpdateStruct(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	assert.Contains(t, src, "type OrderUpdate struct", "Update variant struct must be present")
}

func TestGenerateModels_UserFields(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	// Expected Go field names (PascalCase from snake_case source).
	userFields := []string{
		"CustomerId",
		"Amount",
		"Quantity",
		"IsPaid",
		"PlacedAt",
		"Status",
		"Tags",
		"Metadata",
	}
	for _, field := range userFields {
		assert.Contains(t, src, field, "field %q must appear in generated source", field)
	}
}

func TestGenerateModels_RequiredFieldsNoOmitempty(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	// Required fields on Create struct must not carry omitempty.
	// We look for the Create struct block and check json tags.
	// customer_id and amount are required in the sample schema.
	createIdx := strings.Index(src, "type OrderCreate struct")
	require.True(t, createIdx >= 0, "OrderCreate struct must be present")
	createBlock := src[createIdx:]

	// Find the customer_id json tag inside the Create struct.
	customerIDIdx := strings.Index(createBlock, `"customer_id"`)
	require.True(t, customerIDIdx >= 0, `customer_id json tag must appear in OrderCreate`)

	// The tag should be `json:"customer_id"` (no omitempty).
	tagStr := createBlock[customerIDIdx : customerIDIdx+50]
	assert.NotContains(t, tagStr, "omitempty",
		"required field customer_id in Create struct must not have omitempty")
}

func TestGenerateModels_OptionalFieldsHaveOmitempty(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	createIdx := strings.Index(src, "type OrderCreate struct")
	require.True(t, createIdx >= 0, "OrderCreate struct must be present")
	createBlock := src[createIdx:]

	// quantity is optional; its tag should carry omitempty.
	qtyIdx := strings.Index(createBlock, `"quantity`)
	require.True(t, qtyIdx >= 0, `quantity json tag must appear in OrderCreate`)
	tagStr := createBlock[qtyIdx : qtyIdx+60]
	assert.Contains(t, tagStr, "omitempty",
		"optional field quantity in Create struct must have omitempty")
}

func TestGenerateModels_UpdateFieldsArePointers(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	updateIdx := strings.Index(src, "type OrderUpdate struct")
	require.True(t, updateIdx >= 0, "OrderUpdate struct must be present")
	updateBlock := src[updateIdx:]

	// Find the end of the struct.
	endIdx := strings.Index(updateBlock, "\n}")
	if endIdx > 0 {
		updateBlock = updateBlock[:endIdx]
	}

	// Scalar types (string, int64, float64, bool) must be pointers in Update.
	assert.Contains(t, updateBlock, "*float64", "amount field must be *float64 in Update struct")
	assert.Contains(t, updateBlock, "*int64", "quantity field must be *int64 in Update struct")
	assert.Contains(t, updateBlock, "*bool", "is_paid field must be *bool in Update struct")
}

func TestGenerateModels_EnumType(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	// Enum type declaration.
	assert.Contains(t, src, "type OrderStatus string", "enum type for status field must be declared")

	// Enum const values.
	assert.Contains(t, src, `"pending"`, "pending enum const must be present")
	assert.Contains(t, src, `"shipped"`, "shipped enum const must be present")
	assert.Contains(t, src, `"delivered"`, "delivered enum const must be present")
}

func TestGenerateModels_TimeImport(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	assert.Contains(t, src, `"time"`, `time import must be present because of date-time fields and audit fields`)
}

func TestGenerateModels_GeneratedComment(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	assert.Contains(t, src, "Code generated by stellar-drive", "generated file header must be present")
	assert.Contains(t, src, "DO NOT EDIT", "DO NOT EDIT comment must be present")
}

func TestGenerateModels_ArrayFieldType(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	// tags is []string (array of string items).
	assert.Contains(t, src, "[]string", "array of string items must map to []string")
}

func TestGenerateModels_MapFieldType(t *testing.T) {
	def := sampleSchema()

	src, err := codegen.GenerateModels(def, "order", "")
	require.NoError(t, err)

	// metadata is map[string]any (object without sub-properties).
	assert.Contains(t, src, "map[string]any", "object field without sub-properties must map to map[string]any")
}

// TestGenerateModels_EmptySchema verifies a schema with no user fields still
// produces valid (parseable) Go code.
func TestGenerateModels_EmptySchema(t *testing.T) {
	reg := schema.NewRegistry()
	env := &schema.SchemaEnvelope{
		Name:    "empty",
		Version: "1.0.0",
		Storage: "mongo",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Active: true,
	}
	def, err := reg.Register(env)
	require.NoError(t, err)

	src, genErr := codegen.GenerateModels(def, "empty", "")
	require.NoError(t, genErr)

	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "models.go", src, parser.AllErrors)
	require.NoError(t, parseErr, "empty schema must still produce valid Go:\n%s", src)
}

// TestGenerateModels_MultipleSchemas verifies that independent calls for
// different schemas do not conflict.
func TestGenerateModels_MultipleSchemas(t *testing.T) {
	schemas := []struct {
		name string
		env  *schema.SchemaEnvelope
	}{
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
					},
				},
				Active: true,
			},
		},
		{
			name: "user",
			env: &schema.SchemaEnvelope{
				Name:    "user",
				Version: "1.0.0",
				Storage: "mongo",
				Schema: map[string]any{
					"type":     "object",
					"required": []any{"email"},
					"properties": map[string]any{
						"email": map[string]any{"type": "string", "format": "email"},
						"name":  map[string]any{"type": "string"},
					},
				},
				Active: true,
			},
		},
	}

	for _, tc := range schemas {
		t.Run(tc.name, func(t *testing.T) {
			reg := schema.NewRegistry()
			def, err := reg.Register(tc.env)
			require.NoError(t, err)

			src, genErr := codegen.GenerateModels(def, tc.name, "")
			require.NoError(t, genErr)

			fset := token.NewFileSet()
			_, parseErr := parser.ParseFile(fset, "models.go", src, parser.AllErrors)
			require.NoError(t, parseErr, "schema %q must produce valid Go:\n%s", tc.name, src)
		})
	}
}
