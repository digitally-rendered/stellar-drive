package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

var schemaAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Create a new schema template in the schemas directory",
	Long: `Scaffold a new *.schema.json file in the schemas directory with a
minimal SchemaEnvelope template. Edit the generated file to define your
fields, required properties, and indexes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.ToLower(strings.TrimSpace(args[0]))
		if name == "" {
			return fmt.Errorf("schema add: name must not be empty")
		}

		schemasDir, err := resolveSchemaDir(cfgFile)
		if err != nil {
			return fmt.Errorf("schema add: %w", err)
		}

		return addSchema(name, schemasDir)
	},
}

func init() {
	schemaCmd.AddCommand(schemaAddCmd)
}

// addSchema writes a template SchemaEnvelope JSON file to schemasDir.
func addSchema(name, schemasDir string) error {
	if err := os.MkdirAll(schemasDir, 0o755); err != nil {
		return fmt.Errorf("create schemas directory %q: %w", schemasDir, err)
	}

	fileName := name + ".schema.json"
	path := filepath.Join(schemasDir, fileName)

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("schema file already exists: %s", path)
	}

	now := time.Now().UTC()
	envelope := schema.SchemaEnvelope{
		Name:        name,
		Version:     "1.0.0",
		Description: fmt.Sprintf("%s resource", name),
		Storage:     "mongo",
		Schema: map[string]any{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type":    "object",
			"required": []string{
				"name",
			},
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "A human-readable name",
				},
			},
		},
		Indexes: []schema.IndexDef{
			{Fields: []string{"name"}, Unique: true},
		},
		CreatedAt: now,
		UpdatedAt: now,
		Active:    true,
	}

	b, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal schema template: %w", err)
	}

	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("write schema file %q: %w", path, err)
	}

	fmt.Printf("Created schema template: %s\n", path)
	fmt.Printf("Edit the file to define your fields, then run: stellar run\n")
	return nil
}
