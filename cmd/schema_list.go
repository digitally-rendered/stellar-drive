package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

var schemaListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all schemas in the schemas directory",
	Long: `Scan the schemas directory for *.schema.json files and print their
name, version, storage target, and last-modified time in a table.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		schemasDir, err := resolveSchemaDir(cfgFile)
		if err != nil {
			return fmt.Errorf("schema list: %w", err)
		}
		return listSchemas(schemasDir)
	},
}

func init() {
	schemaCmd.AddCommand(schemaListCmd)
}

// listSchemas scans dir for *.schema.json files and prints a summary table.
func listSchemas(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("schemas directory %q does not exist\n", dir)
			return nil
		}
		return fmt.Errorf("read schemas directory %q: %w", dir, err)
	}

	type row struct {
		name    string
		version string
		storage string
		status  string
		updated time.Time
	}

	var rows []row
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		env, err := schema.LoadFromFile(path)
		if err != nil {
			// Report broken files but continue.
			rows = append(rows, row{
				name:    entry.Name(),
				version: "?",
				storage: "?",
				status:  "PARSE ERROR: " + err.Error(),
			})
			continue
		}

		status := "active"
		if !env.Active && !env.UpdatedAt.IsZero() {
			status = "inactive"
		} else if env.Active {
			status = "active"
		}

		storage := env.Storage
		if storage == "" {
			storage = "mongo"
		}

		rows = append(rows, row{
			name:    env.Name,
			version: env.Version,
			storage: storage,
			status:  status,
			updated: env.UpdatedAt,
		})
	}

	if len(rows) == 0 {
		fmt.Printf("No schemas found in %q\n", dir)
		fmt.Println("Run: stellar schema add <name>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tSTORAGE\tSTATUS\tUPDATED")
	fmt.Fprintln(w, "----\t-------\t-------\t------\t-------")
	for _, r := range rows {
		updated := r.updated.Format(time.RFC3339)
		if r.updated.IsZero() {
			updated = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			r.name, r.version, r.storage, r.status, updated)
	}
	return w.Flush()
}
