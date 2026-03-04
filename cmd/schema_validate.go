package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

var schemaValidateCmd = &cobra.Command{
	Use:   "validate [name]",
	Short: "Validate one or all schemas in the schemas directory",
	Long: `Parse and validate *.schema.json files, reporting any structural or
content errors. When [name] is provided, only that schema is validated.
When omitted, all schemas in the schemas directory are checked.

Exit code is non-zero when any validation error is found.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		schemasDir, err := resolveSchemaDir(cfgFile)
		if err != nil {
			return fmt.Errorf("schema validate: %w", err)
		}

		if len(args) == 1 {
			return validateOne(args[0], schemasDir)
		}
		return validateAll(schemasDir)
	},
}

func init() {
	schemaCmd.AddCommand(schemaValidateCmd)
}

// validateOne validates the named schema from schemasDir.
func validateOne(name, dir string) error {
	path := filepath.Join(dir, name+".schema.json")

	// Allow the caller to pass the full filename too.
	if strings.HasSuffix(name, ".schema.json") {
		path = filepath.Join(dir, name)
	}

	env, err := schema.LoadFromFile(path)
	if err != nil {
		fmt.Printf("FAIL  %s: %v\n", name, err)
		return fmt.Errorf("schema validate: %w", err)
	}

	if err := schema.ValidateEnvelope(env); err != nil {
		fmt.Printf("FAIL  %s: %v\n", name, err)
		return fmt.Errorf("schema validate: %w", err)
	}

	fmt.Printf("OK    %s@%s\n", env.Name, env.Version)
	return nil
}

// validateAll validates every *.schema.json file in dir and reports all errors
// before returning. The returned error is non-nil when at least one schema
// fails validation.
func validateAll(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("schemas directory %q does not exist\n", dir)
			return nil
		}
		return fmt.Errorf("read schemas directory %q: %w", dir, err)
	}

	var failCount int
	var okCount int

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		env, loadErr := schema.LoadFromFile(path)
		if loadErr != nil {
			fmt.Printf("FAIL  %s: %v\n", entry.Name(), loadErr)
			failCount++
			continue
		}

		if valErr := schema.ValidateEnvelope(env); valErr != nil {
			fmt.Printf("FAIL  %s@%s: %v\n", env.Name, env.Version, valErr)
			failCount++
			continue
		}

		fmt.Printf("OK    %s@%s\n", env.Name, env.Version)
		okCount++
	}

	if failCount > 0 {
		return fmt.Errorf("%d of %d schema(s) failed validation",
			failCount, failCount+okCount)
	}

	if okCount == 0 {
		fmt.Printf("No schemas found in %q\n", dir)
		return nil
	}

	fmt.Printf("\n%d schema(s) validated successfully\n", okCount)
	return nil
}
