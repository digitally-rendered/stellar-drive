package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// version is the MCP protocol version this server implements.
const protocolVersion = "2024-11-05"

// serverVersion is the stellar-drive MCP server version.
const serverVersion = "0.1.0"

// Server is an MCP server that exposes the schema registry as AI-callable tools.
// It reads JSON-RPC 2.0 requests from stdin and writes responses to stdout.
type Server struct {
	registry  *schema.Registry
	baseURL   string
	apiPrefix string
	schemaDir string
}

// ServerOption is a functional option for configuring a Server.
type ServerOption func(*Server)

// WithBaseURL sets the base URL of the running stellar-drive HTTP API.
// Used when building example curl commands in query_rest_api.
func WithBaseURL(url string) ServerOption {
	return func(s *Server) {
		s.baseURL = url
	}
}

// WithAPIPrefix sets the API path prefix (e.g. "/api/v1").
func WithAPIPrefix(prefix string) ServerOption {
	return func(s *Server) {
		s.apiPrefix = prefix
	}
}

// WithSchemaDir sets the schemas directory path shown in the generate_sdk tool.
func WithSchemaDir(dir string) ServerOption {
	return func(s *Server) {
		s.schemaDir = dir
	}
}

// NewServer constructs a Server backed by the provided registry.
func NewServer(registry *schema.Registry, opts ...ServerOption) *Server {
	s := &Server{
		registry:  registry,
		baseURL:   "http://localhost:8080",
		apiPrefix: "/api/v1",
		schemaDir: "./schemas",
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Tools returns the complete list of MCP tools this server exposes.
func (s *Server) Tools() []Tool {
	return []Tool{
		{
			Name:        "list_schemas",
			Description: "List all schemas registered in the schema registry, showing name, version, storage backend, and description.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name:        "get_schema",
			Description: "Return the full schema envelope for a named schema, including its JSON Schema definition, indexes, and metadata.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Schema name to retrieve"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "get_schema_dag",
			Description: "Return the dependency graph of all registered schemas. Edges represent $ref relationships between schemas.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name:        "list_versions",
			Description: "List all registered versions of a named schema in ascending order.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Schema name to list versions for"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "describe_entity",
			Description: "Describe the fields, types, formats, and required constraints for a named schema entity.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name": {Type: "string", Description: "Schema name to describe"},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "query_rest_api",
			Description: "Generate a curl example for a given HTTP method and path against the stellar-drive REST API.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"method": {Type: "string", Description: "HTTP method: GET, POST, PUT, PATCH, DELETE"},
					"path":   {Type: "string", Description: "API path, e.g. /pets or /pets/123"},
					"body":   {Type: "string", Description: "Optional JSON request body"},
				},
				Required: []string{"method", "path"},
			},
		},
		{
			Name:        "query_graphql",
			Description: "Return the list of available GraphQL queries and mutations derived from registered schemas.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name:        "get_topology",
			Description: "Return a topology map showing each schema and its configured storage backend.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name:        "create_schema",
			Description: "Register a new schema from a raw JSON schema envelope string.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"name":    {Type: "string", Description: "Schema name"},
					"version": {Type: "string", Description: "Schema version (semver, e.g. 1.0.0)"},
					"schema":  {Type: "string", Description: "JSON string containing the full SchemaEnvelope"},
				},
				Required: []string{"name", "version", "schema"},
			},
		},
		{
			Name:        "validate_schemas",
			Description: "Validate all registered schemas and report any structural issues or missing required fields.",
			InputSchema: InputSchema{Type: "object"},
		},
		{
			Name:        "generate_sdk",
			Description: "Return the CLI command needed to generate typed Go code and an OpenAPI spec from the registered schemas.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"output_dir": {Type: "string", Description: "Output directory for generated files (default: generated)"},
				},
			},
		},
	}
}

// CallTool dispatches a tool call by name with the given arguments.
// It returns a CallToolResult containing text content, or an error for internal failures.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) (*CallToolResult, error) {
	switch name {
	case "list_schemas":
		return s.toolListSchemas(ctx)
	case "get_schema":
		return s.toolGetSchema(ctx, args)
	case "get_schema_dag":
		return s.toolGetSchemaDAG(ctx)
	case "list_versions":
		return s.toolListVersions(ctx, args)
	case "describe_entity":
		return s.toolDescribeEntity(ctx, args)
	case "query_rest_api":
		return s.toolQueryRESTAPI(ctx, args)
	case "query_graphql":
		return s.toolQueryGraphQL(ctx)
	case "get_topology":
		return s.toolGetTopology(ctx)
	case "create_schema":
		return s.toolCreateSchema(ctx, args)
	case "validate_schemas":
		return s.toolValidateSchemas(ctx)
	case "generate_sdk":
		return s.toolGenerateSDK(ctx, args)
	default:
		return errorResult(fmt.Sprintf("unknown tool %q", name)), nil
	}
}

// RunStdio starts the MCP server, reading JSON-RPC 2.0 requests line-by-line
// from stdin and writing responses to stdout. It runs until ctx is cancelled
// or stdin is closed (EOF).
func (s *Server) RunStdio(ctx context.Context) error {
	return s.runIO(ctx, os.Stdin, os.Stdout)
}

// runIO is the testable core of RunStdio. It reads from r and writes to w.
func (s *Server) runIO(ctx context.Context, r io.Reader, w io.Writer) error {
	enc := json.NewEncoder(w)
	scanner := bufio.NewScanner(r)

	// Increase the scanner buffer to handle large schema payloads.
	const maxBuf = 4 * 1024 * 1024 // 4 MiB
	buf := make([]byte, maxBuf)
	scanner.Buffer(buf, maxBuf)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !scanner.Scan() {
			// EOF or scanner error — either way we are done.
			return scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			resp := errorResponse(nil, ErrCodeParseError, "parse error: "+err.Error())
			_ = enc.Encode(resp)
			continue
		}

		resp := s.dispatch(ctx, &req)
		// Notifications (method "initialized") have no ID and no response.
		if resp == nil {
			continue
		}
		_ = enc.Encode(resp)
	}
}

// dispatch routes an incoming JSON-RPC request to the appropriate handler and
// returns the response. Returns nil for notifications that require no response.
func (s *Server) dispatch(ctx context.Context, req *JSONRPCRequest) *JSONRPCResponse {
	switch req.Method {
	case "initialize":
		result := InitializeResult{
			ProtocolVersion: protocolVersion,
			ServerInfo:      ServerInfo{Name: "stellar-drive", Version: serverVersion},
			Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
		}
		return successResponse(req.ID, result)

	case "initialized":
		// Notification — no response required.
		return nil

	case "ping":
		return successResponse(req.ID, map[string]any{})

	case "tools/list":
		return successResponse(req.ID, map[string]any{"tools": s.Tools()})

	case "tools/call":
		name, _ := req.Params["name"].(string)
		if name == "" {
			return errorResponse(req.ID, ErrCodeInvalidParams, "params.name is required")
		}
		rawArgs, _ := req.Params["arguments"].(map[string]any)
		if rawArgs == nil {
			rawArgs = map[string]any{}
		}

		result, err := s.CallTool(ctx, name, rawArgs)
		if err != nil {
			return errorResponse(req.ID, ErrCodeInternalError, err.Error())
		}
		return successResponse(req.ID, result)

	default:
		return errorResponse(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("method %q not found", req.Method))
	}
}

// ---------------------------------------------------------------------------
// Tool implementations
// ---------------------------------------------------------------------------

// toolListSchemas returns a JSON array of all schema names and their latest versions.
func (s *Server) toolListSchemas(_ context.Context) (*CallToolResult, error) {
	defs := s.registry.List()
	if len(defs) == 0 {
		return textResult("No schemas registered."), nil
	}

	type entry struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Storage     string `json:"storage"`
		Description string `json:"description,omitempty"`
	}

	entries := make([]entry, 0, len(defs))
	for _, d := range defs {
		storage := d.Storage
		if storage == "" {
			storage = "mongo"
		}
		entries = append(entries, entry{
			Name:        d.Name,
			Version:     d.Version,
			Storage:     storage,
			Description: d.Description,
		})
	}

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("list_schemas: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// toolGetSchema returns the full schema envelope for the named schema.
func (s *Server) toolGetSchema(_ context.Context, args map[string]any) (*CallToolResult, error) {
	name, ok := stringArg(args, "name")
	if !ok {
		return errorResult("argument 'name' is required"), nil
	}

	env, err := s.registry.GetEnvelope(name, "")
	if err != nil {
		return errorResult(fmt.Sprintf("get_schema: %v", err)), nil
	}

	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("get_schema: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// toolGetSchemaDAG builds and returns the $ref dependency graph for all schemas.
func (s *Server) toolGetSchemaDAG(_ context.Context) (*CallToolResult, error) {
	defs := s.registry.List()

	type dagNode struct {
		Name string   `json:"name"`
		Refs []string `json:"refs"`
	}

	nodes := make([]dagNode, 0, len(defs))
	for _, d := range defs {
		refs := collectRefs(d.RawSchema)
		sort.Strings(refs)
		nodes = append(nodes, dagNode{Name: d.Name, Refs: refs})
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	b, err := json.MarshalIndent(nodes, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("get_schema_dag: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// collectRefs recursively walks a JSON Schema map and returns all $ref values.
func collectRefs(raw map[string]any) []string {
	seen := map[string]bool{}
	walkRefs(raw, seen)
	refs := make([]string, 0, len(seen))
	for r := range seen {
		refs = append(refs, r)
	}
	return refs
}

// walkRefs is the recursive helper for collectRefs.
func walkRefs(node map[string]any, seen map[string]bool) {
	for k, v := range node {
		if k == "$ref" {
			if s, ok := v.(string); ok {
				seen[s] = true
			}
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			walkRefs(child, seen)
		case []any:
			for _, item := range child {
				if m, ok := item.(map[string]any); ok {
					walkRefs(m, seen)
				}
			}
		}
	}
}

// toolListVersions returns all registered versions for a named schema.
func (s *Server) toolListVersions(_ context.Context, args map[string]any) (*CallToolResult, error) {
	name, ok := stringArg(args, "name")
	if !ok {
		return errorResult("argument 'name' is required"), nil
	}

	if !s.registry.Has(name) {
		return errorResult(fmt.Sprintf("schema %q not found", name)), nil
	}

	all := s.registry.ListAll()
	type versionEntry struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	var versions []versionEntry
	for _, d := range all {
		if d.Name == name {
			versions = append(versions, versionEntry{Name: d.Name, Version: d.Version})
		}
	}

	b, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("list_versions: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// toolDescribeEntity returns field-level documentation for the named schema.
func (s *Server) toolDescribeEntity(_ context.Context, args map[string]any) (*CallToolResult, error) {
	name, ok := stringArg(args, "name")
	if !ok {
		return errorResult("argument 'name' is required"), nil
	}

	def, err := s.registry.Get(name, "")
	if err != nil {
		return errorResult(fmt.Sprintf("describe_entity: %v", err)), nil
	}

	type fieldDesc struct {
		Name     string `json:"name"`
		JSONType string `json:"json_type"`
		GoType   string `json:"go_type"`
		Format   string `json:"format,omitempty"`
		Required bool   `json:"required"`
		Ref      string `json:"$ref,omitempty"`
	}

	type entityDesc struct {
		Name           string      `json:"name"`
		Version        string      `json:"version"`
		Description    string      `json:"description,omitempty"`
		Storage        string      `json:"storage"`
		RequiredFields []string    `json:"required_fields"`
		Fields         []fieldDesc `json:"fields"`
	}

	storage := def.Storage
	if storage == "" {
		storage = "mongo"
	}

	fds := make([]fieldDesc, 0, len(def.Fields))
	for _, f := range def.Fields {
		fds = append(fds, fieldDesc{
			Name:     f.Name,
			JSONType: f.JSONType,
			GoType:   f.GoType,
			Format:   f.Format,
			Required: f.Required,
			Ref:      f.Ref,
		})
	}

	desc := entityDesc{
		Name:           def.Name,
		Version:        def.Version,
		Description:    def.Description,
		Storage:        storage,
		RequiredFields: def.RequiredFields,
		Fields:         fds,
	}

	b, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("describe_entity: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// toolQueryRESTAPI generates a curl example for the given HTTP method, path, and body.
func (s *Server) toolQueryRESTAPI(_ context.Context, args map[string]any) (*CallToolResult, error) {
	method, ok := stringArg(args, "method")
	if !ok {
		return errorResult("argument 'method' is required"), nil
	}
	path, ok := stringArg(args, "path")
	if !ok {
		return errorResult("argument 'path' is required"), nil
	}
	body, _ := stringArg(args, "body")

	method = strings.ToUpper(method)
	fullURL := s.baseURL + s.apiPrefix + "/" + strings.TrimPrefix(path, "/")

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "# %s %s\n", method, fullURL)
	_, _ = fmt.Fprintf(&sb, "curl -X %s \\\n", method)
	_, _ = fmt.Fprintf(&sb, "  '%s' \\\n", fullURL)
	sb.WriteString("  -H 'Content-Type: application/json' \\\n")
	sb.WriteString("  -H 'Accept: application/json'")

	if body != "" {
		_, _ = fmt.Fprintf(&sb, " \\\n  -d '%s'", body)
	}
	sb.WriteString("\n\n")

	// Add schema-specific documentation hints when the path matches a schema name.
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && s.registry.Has(parts[0]) {
		def, _ := s.registry.Get(parts[0], "")
		if def != nil {
			_, _ = fmt.Fprintf(&sb, "# Schema: %s@%s\n", def.Name, def.Version)
			_, _ = fmt.Fprintf(&sb, "# Storage: %s\n", def.Storage)
			if len(def.RequiredFields) > 0 {
				_, _ = fmt.Fprintf(&sb, "# Required fields: %s\n", strings.Join(def.RequiredFields, ", "))
			}
		}
	}

	return textResult(sb.String()), nil
}

// toolQueryGraphQL returns the list of available GraphQL queries and mutations
// derived from the registered schemas.
func (s *Server) toolQueryGraphQL(_ context.Context) (*CallToolResult, error) {
	defs := s.registry.List()
	if len(defs) == 0 {
		return textResult("No schemas registered; no GraphQL types available."), nil
	}

	type gqlOp struct {
		Operation string `json:"operation"`
		Type      string `json:"type"` // "query" or "mutation"
	}

	var ops []gqlOp
	for _, d := range defs {
		n := d.Name
		// Queries
		ops = append(ops,
			gqlOp{Operation: fmt.Sprintf("list%ss(filter: FilterInput, limit: Int, offset: Int): [%s!]!", title(n), title(n)), Type: "query"},
			gqlOp{Operation: fmt.Sprintf("get%s(id: ID!): %s", title(n), title(n)), Type: "query"},
		)
		// Mutations
		ops = append(ops,
			gqlOp{Operation: fmt.Sprintf("create%s(input: Create%sInput!): %s!", title(n), title(n), title(n)), Type: "mutation"},
			gqlOp{Operation: fmt.Sprintf("update%s(id: ID!, input: Update%sInput!): %s!", title(n), title(n), title(n)), Type: "mutation"},
			gqlOp{Operation: fmt.Sprintf("delete%s(id: ID!): Boolean!", title(n)), Type: "mutation"},
		)
	}

	b, err := json.MarshalIndent(ops, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("query_graphql: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// toolGetTopology returns a topology map of schema → storage backend.
func (s *Server) toolGetTopology(_ context.Context) (*CallToolResult, error) {
	defs := s.registry.List()

	type topologyEntry struct {
		Schema     string `json:"schema"`
		Version    string `json:"version"`
		Storage    string `json:"storage"`
		Collection string `json:"collection,omitempty"`
	}

	entries := make([]topologyEntry, 0, len(defs))
	for _, d := range defs {
		storage := d.Storage
		if storage == "" {
			storage = "mongo"
		}
		entries = append(entries, topologyEntry{
			Schema:     d.Name,
			Version:    d.Version,
			Storage:    storage,
			Collection: d.Collection,
		})
	}

	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("get_topology: marshal: %w", err)
	}
	return textResult(string(b)), nil
}

// toolCreateSchema parses a JSON schema envelope string and registers it.
func (s *Server) toolCreateSchema(_ context.Context, args map[string]any) (*CallToolResult, error) {
	rawJSON, ok := stringArg(args, "schema")
	if !ok {
		return errorResult("argument 'schema' is required"), nil
	}

	var env schema.SchemaEnvelope
	if err := json.Unmarshal([]byte(rawJSON), &env); err != nil {
		return errorResult(fmt.Sprintf("create_schema: invalid JSON: %v", err)), nil
	}

	// Allow name/version overrides from top-level args.
	if n, ok := stringArg(args, "name"); ok && n != "" {
		env.Name = n
	}
	if v, ok := stringArg(args, "version"); ok && v != "" {
		env.Version = v
	}
	env.Active = true

	def, err := s.registry.Register(&env)
	if err != nil {
		return errorResult(fmt.Sprintf("create_schema: %v", err)), nil
	}

	return textResult(fmt.Sprintf("Registered schema %q version %q with %d field(s).",
		def.Name, def.Version, len(def.Fields))), nil
}

// toolValidateSchemas validates all registered schemas and reports issues.
func (s *Server) toolValidateSchemas(_ context.Context) (*CallToolResult, error) {
	defs := s.registry.List()
	if len(defs) == 0 {
		return textResult("No schemas registered."), nil
	}

	var sb strings.Builder
	var failCount, okCount int

	for _, def := range defs {
		env, err := s.registry.GetEnvelope(def.Name, def.Version)
		if err != nil {
			_, _ = fmt.Fprintf(&sb, "FAIL  %s@%s: %v\n", def.Name, def.Version, err)
			failCount++
			continue
		}
		if err := schema.ValidateEnvelope(env); err != nil {
			_, _ = fmt.Fprintf(&sb, "FAIL  %s@%s: %v\n", def.Name, def.Version, err)
			failCount++
			continue
		}
		_, _ = fmt.Fprintf(&sb, "OK    %s@%s\n", def.Name, def.Version)
		okCount++
	}

	_, _ = fmt.Fprintf(&sb, "\n%d OK, %d FAIL (total: %d schema(s))\n",
		okCount, failCount, okCount+failCount)

	result := &CallToolResult{
		Content: []TextContent{{Type: "text", Text: sb.String()}},
		IsError: failCount > 0,
	}
	return result, nil
}

// toolGenerateSDK returns the CLI command to run to generate SDK code.
func (s *Server) toolGenerateSDK(_ context.Context, args map[string]any) (*CallToolResult, error) {
	outputDir := "generated"
	if v, ok := stringArg(args, "output_dir"); ok && v != "" {
		outputDir = v
	}

	cmd := fmt.Sprintf(
		"stellar-drive generate --schemas %s --output %s",
		s.schemaDir, outputDir,
	)

	var sb strings.Builder
	sb.WriteString("Run the following command to generate typed Go code and an OpenAPI spec:\n\n")
	sb.WriteString("  " + cmd + "\n\n")
	sb.WriteString("This produces:\n")
	sb.WriteString("  - Typed Go structs (Document, Create, Update variants)\n")
	sb.WriteString("  - Typed repository and service wrappers\n")
	sb.WriteString("  - Typed HTTP handlers\n")
	sb.WriteString("  - GraphQL SDL files\n")
	sb.WriteString("  - OpenAPI 3.1 JSON spec\n")
	_, _ = fmt.Fprintf(&sb, "\nGenerated files are written to: %s/\n", outputDir)

	return textResult(sb.String()), nil
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func successResponse(id any, result any) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

func errorResponse(id any, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

func textResult(text string) *CallToolResult {
	return &CallToolResult{
		Content: []TextContent{{Type: "text", Text: text}},
	}
}

func errorResult(text string) *CallToolResult {
	return &CallToolResult{
		Content: []TextContent{{Type: "text", Text: text}},
		IsError: true,
	}
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

func stringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// title returns s with its first letter uppercased.
func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
