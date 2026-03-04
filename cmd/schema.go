package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/config"
)

// schemaCmd is the parent command for all schema-related subcommands.
var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Manage stellar-drive schemas",
	Long:  "Commands for adding, listing, and validating JSON Schema definitions.",
}

func init() {
	rootCmd.AddCommand(schemaCmd)
}

// resolveSchemaDir loads the config at cfgFile and returns the schemas
// directory path. When the config cannot be loaded it returns the default
// "./schemas" directory so the schema subcommands remain usable without a
// fully valid config.
func resolveSchemaDir(cfgPath string) (string, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		// Fall back to the default so schema commands remain useful when no
		// config file is present (e.g. before "stellar init").
		fmt.Printf("warning: could not load config %q, using default schemas directory\n", cfgPath)
		return "./schemas", nil //nolint:nilerr // intentional fallback
	}
	dir := cfg.Schemas.Dir
	if dir == "" {
		dir = "./schemas"
	}
	return dir, nil
}
