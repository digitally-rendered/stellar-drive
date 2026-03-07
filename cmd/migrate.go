package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/sql"
	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// migrateCmd is the parent command that groups SQL migration subcommands.
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration commands",
	Long:  "Commands for applying and inspecting SQL schema migrations.",
}

// migrateUpCmd applies all pending migrations for registered schemas.
var migrateUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending migrations",
	Long: `Load the configuration file, read schemas from the configured directory,
open the SQL database, and create any tables and indexes that do not yet exist.

Already-applied migrations are skipped (idempotent).`,
	RunE: runMigrateUp,
}

// migrateStatusCmd shows which migrations have been applied.
var migrateStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status",
	Long:  `Display a table of all migrations recorded in the _migrations tracking table.`,
	RunE:  runMigrateStatus,
}

func init() {
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateStatusCmd)
	rootCmd.AddCommand(migrateCmd)
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runMigrateUp(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	migrator, conn, err := buildMigrator()
	if err != nil {
		return err
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close

	reg, err := loadSchemas()
	if err != nil {
		return err
	}

	if err := migrator.MigrateAll(ctx, reg); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}

	// Report what was applied.
	records, err := migrator.Status(ctx)
	if err != nil {
		return fmt.Errorf("migrate up: fetch status: %w", err)
	}

	fmt.Printf("Applied migrations: %d\n\n", len(records))
	printMigrationTable(records)
	return nil
}

func runMigrateStatus(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	migrator, conn, err := buildMigrator()
	if err != nil {
		return err
	}
	defer conn.Close(ctx) //nolint:errcheck // best-effort close

	// Ensure the tracking table exists before querying it; if no migrations
	// have ever been applied this prevents a driver error.
	if err := migrator.EnsureMigrationTable(ctx); err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}

	records, err := migrator.Status(ctx)
	if err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}

	if len(records) == 0 {
		fmt.Println("No migrations have been applied.")
		return nil
	}

	printMigrationTable(records)
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildMigrator loads configuration, opens a SQL connection, and returns a
// ready-to-use Migrator together with the open Connection (caller must close).
func buildMigrator() (*sql.Migrator, *sql.Connection, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: load config: %w", err)
	}

	if cfg.SQL.Driver == "" {
		return nil, nil, fmt.Errorf("migrate: sql.driver is not set in %q", cfgFile)
	}
	if cfg.SQL.DSN == "" {
		return nil, nil, fmt.Errorf("migrate: sql.dsn is not set in %q", cfgFile)
	}

	conn, err := sql.NewConnection(cfg.SQL.Driver, cfg.SQL.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: open SQL connection: %w", err)
	}

	dialect := sql.NewDialect(cfg.SQL.Driver)
	migrator := sql.NewMigrator(conn, dialect)
	return migrator, conn, nil
}

// loadSchemas reads the schema directory from config and registers all schemas
// into a fresh registry.
func loadSchemas() (*schema.Registry, error) {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("migrate: load config: %w", err)
	}

	dir := cfg.Schemas.Dir
	if dir == "" {
		dir = "./schemas"
	}

	reg := schema.NewRegistry()
	if err := schema.LoadFromDir(reg, dir); err != nil {
		return nil, fmt.Errorf("migrate: load schemas from %q: %w", dir, err)
	}
	return reg, nil
}

// printMigrationTable renders migration records as a tab-separated table to
// stdout.
func printMigrationTable(records []sql.MigrationRecord) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tSCHEMA\tDESCRIPTION\tAPPLIED AT")
	for _, r := range records {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n",
			r.Version,
			r.SchemaName,
			r.Description,
			r.AppliedAt.Format("2006-01-02 15:04:05 UTC"),
		)
	}
	w.Flush()
}
