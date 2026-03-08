// Package codegen orchestrates Go source file generation from registered JSON
// Schema definitions. Each schema produces typed structs, repository wrappers,
// service wrappers, HTTP handlers, GraphQL schema text, and an OpenAPI spec.
package codegen

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// Generator drives code generation for all schemas in a Registry.
type Generator struct {
	registry  *schema.Registry
	outputDir string
	// modulePath is the Go module path used in generated import statements.
	modulePath string
}

// NewGenerator constructs a Generator that writes files to outputDir.
// modulePath should be the module path declared in go.mod (e.g.
// "github.com/digitally-rendered/stellar-drive").
func NewGenerator(registry *schema.Registry, outputDir, modulePath string) *Generator {
	return &Generator{
		registry:   registry,
		outputDir:  outputDir,
		modulePath: modulePath,
	}
}

// Generate iterates every registered schema and generates all artefacts for
// each one. Files are written under outputDir/<schemaName>/.
func (g *Generator) Generate() error {
	names := g.registry.Names()
	for _, name := range names {
		if err := g.GenerateSchema(name); err != nil {
			return fmt.Errorf("generate schema %q: %w", name, err)
		}
	}
	return nil
}

// GenerateSchema generates all artefacts for a single named schema (latest
// version). The output is placed in outputDir/<schemaName>/.
func (g *Generator) GenerateSchema(name string) error {
	def, err := g.registry.Get(name, "")
	if err != nil {
		return fmt.Errorf("get schema %q: %w", name, err)
	}

	dir := filepath.Join(g.outputDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output dir %q: %w", dir, err)
	}

	pkgName := sanitizePackageName(name)

	// convert.go – shared helpers used by repository.go and service.go
	// Use strings.Replace instead of fmt.Sprintf to avoid go vet misreading
	// the %v format verbs inside the template as belonging to this call.
	convertSrc := strings.Replace(convertHelpersSource, "PACKAGE_NAME", pkgName, 1)
	if err := writeGoFile(filepath.Join(dir, "convert.go"), convertSrc); err != nil {
		return fmt.Errorf("write convert.go for %q: %w", name, err)
	}

	// models.go
	modelsCode, err := GenerateModels(def, pkgName, g.modulePath)
	if err != nil {
		return fmt.Errorf("generate models for %q: %w", name, err)
	}
	if err := writeGoFile(filepath.Join(dir, "models.go"), modelsCode); err != nil {
		return fmt.Errorf("write models.go for %q: %w", name, err)
	}

	// repository.go
	repoCode, err := GenerateRepository(def, pkgName, g.modulePath)
	if err != nil {
		return fmt.Errorf("generate repository for %q: %w", name, err)
	}
	if err := writeGoFile(filepath.Join(dir, "repository.go"), repoCode); err != nil {
		return fmt.Errorf("write repository.go for %q: %w", name, err)
	}

	// service.go
	svcCode, err := GenerateService(def, pkgName, g.modulePath)
	if err != nil {
		return fmt.Errorf("generate service for %q: %w", name, err)
	}
	if err := writeGoFile(filepath.Join(dir, "service.go"), svcCode); err != nil {
		return fmt.Errorf("write service.go for %q: %w", name, err)
	}

	// handler.go
	handlerCode, err := GenerateHandler(def, pkgName, g.modulePath)
	if err != nil {
		return fmt.Errorf("generate handler for %q: %w", name, err)
	}
	if err := writeGoFile(filepath.Join(dir, "handler.go"), handlerCode); err != nil {
		return fmt.Errorf("write handler.go for %q: %w", name, err)
	}

	// schema.graphqls
	gqlCode, err := GenerateGraphQL(def)
	if err != nil {
		return fmt.Errorf("generate graphql for %q: %w", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.graphqls"), []byte(gqlCode), 0o644); err != nil {
		return fmt.Errorf("write schema.graphqls for %q: %w", name, err)
	}

	return nil
}

// writeGoFile formats src with go/format and writes it to path.
func writeGoFile(path, src string) error {
	formatted, err := format.Source([]byte(src))
	if err != nil {
		// Write the unformatted source so the caller can inspect it.
		_ = os.WriteFile(path, []byte(src), 0o644)
		return fmt.Errorf("format %q: %w", path, err)
	}
	return os.WriteFile(path, formatted, 0o644)
}

// sanitizePackageName converts an arbitrary schema name to a valid lowercase
// Go package identifier.
func sanitizePackageName(name string) string {
	b := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+32) // toLower
		case c >= '0' && c <= '9':
			if len(b) > 0 { // digits not allowed at start
				b = append(b, c)
			}
		case c == '_':
			b = append(b, c)
			// drop everything else (hyphens, spaces, dots …)
		}
	}
	if len(b) == 0 {
		return "generated"
	}
	return string(b)
}
