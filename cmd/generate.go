package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/digitally-rendered/stellar-drive/pkg/codegen"
	"github.com/digitally-rendered/stellar-drive/pkg/codegen/sdk"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

var (
	generateOutput  string
	generateSchemas string
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate typed Go code and API specs from JSON Schema definitions",
	Long: `Reads *.schema.json files from the schemas directory and produces:
  - Typed Go structs (Document, Create, Update variants)
  - Typed repository and service wrappers
  - Typed HTTP handlers
  - GraphQL SDL files
  - OpenAPI 3.1 JSON spec

Generated files are written to the output directory (default: generated/).`,
	RunE: runGenerate,
}

func init() {
	generateCmd.Flags().StringVar(
		&generateOutput,
		"output",
		"generated",
		"Directory to write generated files into",
	)
	generateCmd.Flags().StringVar(
		&generateSchemas,
		"schemas",
		"schemas",
		"Directory containing *.schema.json files",
	)
	rootCmd.AddCommand(generateCmd)
}

func runGenerate(_ *cobra.Command, _ []string) error {
	modulePath, err := resolveModulePath()
	if err != nil {
		return fmt.Errorf("generate: resolve module path: %w", err)
	}

	reg := schema.NewRegistry()
	if err := schema.LoadFromDir(reg, generateSchemas); err != nil {
		return fmt.Errorf("generate: load schemas from %q: %w", generateSchemas, err)
	}

	if len(reg.Names()) == 0 {
		fmt.Printf("No schemas found in %q; nothing to generate.\n", generateSchemas)
		return nil
	}

	// Generate per-schema Go code.
	gen := codegen.NewGenerator(reg, generateOutput, modulePath)
	if err := gen.Generate(); err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	// Generate the combined OpenAPI spec.
	spec, err := sdk.GenerateOpenAPI(reg, modulePath, "1.0.0")
	if err != nil {
		return fmt.Errorf("generate: openapi: %w", err)
	}

	specPath := filepath.Join(generateOutput, "openapi.json")
	specBytes, err := sdk.MarshalJSON(spec)
	if err != nil {
		return fmt.Errorf("generate: marshal openapi: %w", err)
	}
	if err := os.WriteFile(specPath, append(specBytes, '\n'), 0o644); err != nil {
		return fmt.Errorf("generate: write openapi.json: %w", err)
	}

	fmt.Printf("Generated code for %d schema(s) → %s\n", len(reg.Names()), generateOutput)
	fmt.Printf("OpenAPI spec → %s\n", specPath)
	return nil
}

// resolveModulePath reads the nearest go.mod and returns the module path.
// It walks up from the current working directory.
func resolveModulePath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(candidate)
		if err == nil {
			f, parseErr := modfile.Parse(candidate, data, nil)
			if parseErr != nil {
				return "", fmt.Errorf("parse go.mod %q: %w", candidate, parseErr)
			}
			return f.Module.Mod.Path, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("go.mod not found in any parent directory")
}
