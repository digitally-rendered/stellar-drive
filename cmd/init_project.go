package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init <project-name>",
	Short: "Scaffold a new stellar-drive project",
	Long: `Create a new project directory containing:
  - go.mod
  - stellar.yaml  (default configuration)
  - schemas/      (empty schemas directory with a sample schema)
  - main.go       (imports and starts the engine)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		return scaffoldProject(name)
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

// scaffoldProject creates the project skeleton under a directory named after
// projectName in the current working directory.
func scaffoldProject(projectName string) error {
	// Sanitise the name for use as a Go module path segment and directory name.
	dirName := strings.ToLower(projectName)
	dirName = strings.ReplaceAll(dirName, " ", "-")

	if err := os.MkdirAll(filepath.Join(dirName, "schemas"), 0o755); err != nil {
		return fmt.Errorf("init: create project directory: %w", err)
	}

	type tplData struct {
		ProjectName string
		DirName     string
	}
	data := tplData{ProjectName: projectName, DirName: dirName}

	files := []struct {
		path    string
		content string
	}{
		{
			path: filepath.Join(dirName, "go.mod"),
			content: `module {{.DirName}}

go 1.24.0

require github.com/digitally-rendered/stellar-drive v0.1.0
`,
		},
		{
			path:    filepath.Join(dirName, "stellar.yaml"),
			content: stellarYAMLTemplate,
		},
		{
			path:    filepath.Join(dirName, "main.go"),
			content: mainGoTemplate,
		},
		{
			path:    filepath.Join(dirName, "schemas", "example.schema.json"),
			content: exampleSchemaTemplate,
		},
	}

	for _, f := range files {
		if err := writeTemplate(f.path, f.content, data); err != nil {
			return err
		}
		fmt.Printf("  created %s\n", f.path)
	}

	fmt.Printf("\nProject %q initialised. Next steps:\n", projectName)
	fmt.Printf("  cd %s\n", dirName)
	fmt.Printf("  go mod tidy\n")
	fmt.Printf("  stellar run\n")
	return nil
}

// writeTemplate executes the Go template content with data and writes it to
// path. The file is created if it does not exist; an existing file is not
// overwritten.
func writeTemplate(path, content string, data any) error {
	if _, err := os.Stat(path); err == nil {
		// File already exists; skip without error so re-running init is safe.
		return nil
	}

	tpl, err := template.New(filepath.Base(path)).Parse(content)
	if err != nil {
		return fmt.Errorf("init: parse template for %s: %w", path, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("init: create file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if err := tpl.Execute(f, data); err != nil {
		return fmt.Errorf("init: render template for %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Inline templates
// ---------------------------------------------------------------------------

const stellarYAMLTemplate = `project:
  name: {{.ProjectName}}
  version: "0.1.0"

server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: 30s
  write_timeout: 30s
  graceful_shutdown: 15s
  api_prefix: /api/v1

schemas:
  dir: ./schemas
  hot_reload: false

mongo:
  uri: mongodb://localhost:27017
  database: {{.DirName}}
  max_pool_size: 100

graphql:
  enabled: false
  path: /graphql

middleware:
  cors:
    allowed_origins:
      - "*"
  security_headers: true
  rate_limit:
    requests_per_second: 100
    burst: 50
`

const mainGoTemplate = `// Package main is the entry point for the {{.ProjectName}} service.
//
// The default main boots the stellar-drive engine with the configuration in
// stellar.yaml and the schemas under ./schemas. Every extension point is
// reachable through engine.Option values — no need to fork stellar-drive to
// add guards, validators, custom endpoints, or event handlers.
//
// Uncomment the blocks below as you grow the service.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/engine"
	// Extension-point imports — uncomment as needed.
	// "net/http"
	// "github.com/digitally-rendered/stellar-drive/pkg/core/event"
	// "github.com/digitally-rendered/stellar-drive/pkg/core/registry"
)

func main() {
	cfg, err := config.Load("stellar.yaml")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	opts := []engine.Option{
		// --- Custom handlers, guards, validators, transforms ------------------
		// funcReg := registry.NewFunctionRegistry()
		//
		// // Guard: deny delete unless caller has the "admin" role header.
		// funcReg.RegisterGuard("pet", registry.OpDelete, registry.Scope{},
		//     func(ctx context.Context, r *http.Request) error {
		//         if r.Header.Get("X-Role") != "admin" {
		//             return fmt.Errorf("admin only")
		//         }
		//         return nil
		//     })
		//
		// // Validator: reject empty names before they hit the repository.
		// funcReg.RegisterValidator("pet", registry.OpCreate, registry.Scope{},
		//     func(ctx context.Context, schemaName string, data map[string]any) error {
		//         if s, _ := data["name"].(string); s == "" {
		//             return fmt.Errorf("name is required")
		//         }
		//         return nil
		//     })
		//
		// // Transform: normalise input before persistence.
		// funcReg.RegisterTransform("pet", registry.OpCreate, registry.Scope{},
		//     func(ctx context.Context, schemaName string, data map[string]any) (map[string]any, error) {
		//         if s, ok := data["name"].(string); ok {
		//             data["name"] = strings.TrimSpace(s)
		//         }
		//         return data, nil
		//     })
		//
		// // Handler override: replace the default list endpoint entirely.
		// funcReg.RegisterHandler("pet", registry.OpList, registry.Scope{}, myCustomListHandler)
		//
		// opts = append(opts, engine.WithFunctionRegistry(funcReg))

		// --- Event subscriptions ---------------------------------------------
		// bus := event.NewBus()
		// bus.Subscribe("pet.created", func(ctx context.Context, e event.Event) error {
		//     slog.Info("pet created", "entity_id", e.EntityID)
		//     return nil
		// })
		// opts = append(opts, engine.WithEventBus(bus))

		// --- Custom middleware (runs after built-in stack) -------------------
		// opts = append(opts, engine.WithMiddleware(myMetricsMiddleware))

		// --- Custom policy evaluator (inject OPA client, etc.) ---------------
		// opts = append(opts, engine.WithPolicyEvaluator(myPolicyEvaluator))
	}

	eng := engine.New(cfg, opts...)
	if err := eng.Start(context.Background()); err != nil {
		slog.Error("engine stopped with error", "error", err)
		os.Exit(1)
	}
}
`

const exampleSchemaTemplate = `{
  "name": "example",
  "version": "1.0.0",
  "description": "Example schema — replace with your own",
  "storage": "mongo",
  "schema": {
    "$schema": "http://json-schema.org/draft-07/schema#",
    "type": "object",
    "required": ["name"],
    "properties": {
      "name": {
        "type": "string",
        "description": "A human-readable name"
      },
      "description": {
        "type": "string",
        "description": "Optional description"
      },
      "active": {
        "type": "boolean",
        "default": true
      }
    }
  },
  "indexes": [
    { "fields": ["name"], "unique": true }
  ]
}
`
