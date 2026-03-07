package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"github.com/digitally-rendered/stellar-drive/pkg/mcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP (Model Context Protocol) server",
	Long: `Starts an MCP server that communicates over stdio using JSON-RPC 2.0,
exposing schema registry tools for AI agents.

The server reads requests from stdin and writes JSON-RPC responses to stdout.
It loads all schemas from the configured schemas directory on startup.

Supported tools:
  list_schemas       List all registered schemas
  get_schema         Retrieve a full schema envelope by name
  get_schema_dag     Show the $ref dependency graph
  list_versions      List all versions of a schema
  describe_entity    Describe fields and types of a schema
  query_rest_api     Generate a curl example for the REST API
  query_graphql      List available GraphQL queries and mutations
  get_topology       Show schema-to-storage-backend mapping
  create_schema      Register a new schema from JSON
  validate_schemas   Validate all registered schemas
  generate_sdk       Print the command to generate typed SDK code`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		// Non-fatal: fall back to defaults so the MCP server remains useful
		// in environments without a stellar.yaml.
		slog.Warn("mcp: could not load config, using defaults", "error", err)
		cfg = &config.StellarConfig{}
	}

	schemasDir := cfg.Schemas.Dir
	if schemasDir == "" {
		schemasDir = "./schemas"
	}

	apiPrefix := cfg.Server.APIPrefix
	if apiPrefix == "" {
		apiPrefix = "/api/v1"
	}

	baseURL := fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port)
	if cfg.Server.Host == "" {
		baseURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	}
	if cfg.Server.Port == 0 {
		baseURL = "http://localhost:8080"
	}

	reg := schema.NewRegistry()

	if _, err := os.Stat(schemasDir); err == nil {
		if loadErr := schema.LoadFromDir(reg, schemasDir); loadErr != nil {
			slog.Warn("mcp: failed to load some schemas", "dir", schemasDir, "error", loadErr)
		}
	}

	slog.Info("mcp: server starting",
		"schemas", len(reg.Names()),
		"schemas_dir", schemasDir,
	)

	srv := mcp.NewServer(reg,
		mcp.WithBaseURL(baseURL),
		mcp.WithAPIPrefix(apiPrefix),
		mcp.WithSchemaDir(schemasDir),
	)

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return srv.RunStdio(ctx)
}
