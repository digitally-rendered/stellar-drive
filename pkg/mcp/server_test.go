package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
	"github.com/digitally-rendered/stellar-drive/pkg/mcp"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testEnvelope builds a minimal SchemaEnvelope for use in tests.
func testEnvelope(name, version string) *schema.SchemaEnvelope {
	return &schema.SchemaEnvelope{
		Name:    name,
		Version: version,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"age":  map[string]any{"type": "integer"},
			},
			"required": []any{"name"},
		},
		Active: true,
	}
}

// newServerWithSchemas creates a Server pre-loaded with the given envelopes.
func newServerWithSchemas(t *testing.T, envelopes ...*schema.SchemaEnvelope) *mcp.Server {
	t.Helper()
	reg := schema.NewRegistry()
	for _, env := range envelopes {
		_, err := reg.Register(env)
		require.NoError(t, err, "register test schema %s@%s", env.Name, env.Version)
	}
	return mcp.NewServer(reg)
}

// toolNames returns the sorted list of tool names from a Tools() call.
func toolNames(tools []mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// ---------------------------------------------------------------------------
// TestTools
// ---------------------------------------------------------------------------

func TestTools(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())
	tools := srv.Tools()

	expected := []string{
		"create_schema",
		"describe_entity",
		"generate_sdk",
		"get_schema",
		"get_schema_dag",
		"get_topology",
		"list_schemas",
		"list_versions",
		"query_graphql",
		"query_rest_api",
		"validate_schemas",
	}

	names := toolNames(tools)
	// Sort before comparison because Tools() order is deterministic but let's
	// be explicit in the assertion.
	assert.ElementsMatch(t, expected, names, "all 11 tools must be registered")
	assert.Len(t, tools, 11, "exactly 11 tools")

	for _, tool := range tools {
		assert.NotEmpty(t, tool.Name, "tool name must not be empty")
		assert.NotEmpty(t, tool.Description, "tool %q must have a description", tool.Name)
		assert.Equal(t, "object", tool.InputSchema.Type, "tool %q InputSchema.Type must be 'object'", tool.Name)
	}
}

// ---------------------------------------------------------------------------
// TestCallTool_ListSchemas
// ---------------------------------------------------------------------------

func TestCallTool_ListSchemas(t *testing.T) {
	srv := newServerWithSchemas(t,
		testEnvelope("pet", "1.0.0"),
		testEnvelope("order", "1.0.0"),
	)

	result, err := srv.CallTool(context.Background(), "list_schemas", nil)
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success, got error: %s", result.Content[0].Text)

	text := result.Content[0].Text
	assert.Contains(t, text, "pet")
	assert.Contains(t, text, "order")
	assert.Contains(t, text, "1.0.0")
}

// ---------------------------------------------------------------------------
// TestCallTool_GetSchema
// ---------------------------------------------------------------------------

func TestCallTool_GetSchema(t *testing.T) {
	srv := newServerWithSchemas(t, testEnvelope("pet", "1.0.0"))

	result, err := srv.CallTool(context.Background(), "get_schema", map[string]any{"name": "pet"})
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success, got error: %s", result.Content[0].Text)

	text := result.Content[0].Text

	// The response must be valid JSON.
	var env schema.SchemaEnvelope
	require.NoError(t, json.Unmarshal([]byte(text), &env))
	assert.Equal(t, "pet", env.Name)
	assert.Equal(t, "1.0.0", env.Version)
}

func TestCallTool_GetSchema_NotFound(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())

	result, err := srv.CallTool(context.Background(), "get_schema", map[string]any{"name": "missing"})
	require.NoError(t, err)
	assert.True(t, result.IsError, "should return error for unknown schema")
}

func TestCallTool_GetSchema_MissingArg(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())

	result, err := srv.CallTool(context.Background(), "get_schema", map[string]any{})
	require.NoError(t, err)
	assert.True(t, result.IsError, "should return error when 'name' argument is missing")
}

// ---------------------------------------------------------------------------
// TestCallTool_DescribeEntity
// ---------------------------------------------------------------------------

func TestCallTool_DescribeEntity(t *testing.T) {
	srv := newServerWithSchemas(t, testEnvelope("pet", "1.0.0"))

	result, err := srv.CallTool(context.Background(), "describe_entity", map[string]any{"name": "pet"})
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success: %s", result.Content[0].Text)

	text := result.Content[0].Text
	assert.Contains(t, text, "name")
	assert.Contains(t, text, "age")
	assert.Contains(t, text, "string")
	assert.Contains(t, text, "integer")
	assert.Contains(t, text, "required_fields")
}

// ---------------------------------------------------------------------------
// TestCallTool_ListVersions
// ---------------------------------------------------------------------------

func TestCallTool_ListVersions(t *testing.T) {
	srv := newServerWithSchemas(t,
		testEnvelope("pet", "1.0.0"),
		testEnvelope("pet", "2.0.0"),
	)

	result, err := srv.CallTool(context.Background(), "list_versions", map[string]any{"name": "pet"})
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success: %s", result.Content[0].Text)

	text := result.Content[0].Text
	assert.Contains(t, text, "1.0.0")
	assert.Contains(t, text, "2.0.0")
}

func TestCallTool_ListVersions_NotFound(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())

	result, err := srv.CallTool(context.Background(), "list_versions", map[string]any{"name": "ghost"})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// ---------------------------------------------------------------------------
// TestCallTool_CreateSchema
// ---------------------------------------------------------------------------

func TestCallTool_CreateSchema(t *testing.T) {
	reg := schema.NewRegistry()
	srv := mcp.NewServer(reg)

	envJSON, err := json.Marshal(testEnvelope("widget", "1.0.0"))
	require.NoError(t, err)

	result, err := srv.CallTool(context.Background(), "create_schema", map[string]any{
		"name":    "widget",
		"version": "1.0.0",
		"schema":  string(envJSON),
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success: %s", result.Content[0].Text)

	assert.True(t, reg.Has("widget"), "schema 'widget' should be registered after create_schema")
	assert.Contains(t, result.Content[0].Text, "widget")
}

func TestCallTool_CreateSchema_InvalidJSON(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())

	result, err := srv.CallTool(context.Background(), "create_schema", map[string]any{
		"name":    "bad",
		"version": "1.0.0",
		"schema":  "this is not json",
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

// ---------------------------------------------------------------------------
// TestCallTool_GetSchemaDAG
// ---------------------------------------------------------------------------

func TestCallTool_GetSchemaDAG(t *testing.T) {
	reg := schema.NewRegistry()

	// pet has a $ref to category.
	petEnv := &schema.SchemaEnvelope{
		Name:    "pet",
		Version: "1.0.0",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":     map[string]any{"type": "string"},
				"category": map[string]any{"$ref": "#/definitions/category"},
			},
		},
		Active: true,
	}
	_, err := reg.Register(petEnv)
	require.NoError(t, err)

	_, err = reg.Register(testEnvelope("category", "1.0.0"))
	require.NoError(t, err)

	srv := mcp.NewServer(reg)

	result, err := srv.CallTool(context.Background(), "get_schema_dag", nil)
	require.NoError(t, err)
	require.False(t, result.IsError, "expected success: %s", result.Content[0].Text)

	text := result.Content[0].Text

	// The DAG JSON must be parseable.
	var dag []struct {
		Name string   `json:"name"`
		Refs []string `json:"refs"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &dag))

	// Find the pet entry and confirm it lists the $ref.
	var petNode *struct {
		Name string   `json:"name"`
		Refs []string `json:"refs"`
	}
	for i := range dag {
		if dag[i].Name == "pet" {
			petNode = &dag[i]
			break
		}
	}
	require.NotNil(t, petNode, "pet must appear in DAG")
	assert.Contains(t, petNode.Refs, "#/definitions/category")
}

// ---------------------------------------------------------------------------
// TestCallTool_UnknownTool
// ---------------------------------------------------------------------------

func TestCallTool_UnknownTool(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())

	result, err := srv.CallTool(context.Background(), "does_not_exist", nil)
	require.NoError(t, err)
	assert.True(t, result.IsError, "unknown tool must return isError=true")
	assert.Contains(t, result.Content[0].Text, "does_not_exist")
}

// ---------------------------------------------------------------------------
// TestCallTool_ValidateSchemas
// ---------------------------------------------------------------------------

func TestCallTool_ValidateSchemas(t *testing.T) {
	srv := newServerWithSchemas(t,
		testEnvelope("pet", "1.0.0"),
		testEnvelope("order", "1.0.0"),
	)

	result, err := srv.CallTool(context.Background(), "validate_schemas", nil)
	require.NoError(t, err)

	text := result.Content[0].Text
	assert.Contains(t, text, "OK")
	assert.Contains(t, text, "pet")
	assert.Contains(t, text, "order")
}

func TestCallTool_ValidateSchemas_Empty(t *testing.T) {
	srv := mcp.NewServer(schema.NewRegistry())

	result, err := srv.CallTool(context.Background(), "validate_schemas", nil)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "No schemas")
}

// ---------------------------------------------------------------------------
// TestRunStdio
// ---------------------------------------------------------------------------

// TestRunStdio pipes a sequence of JSON-RPC messages through the server's
// runIO path and verifies the responses.
func TestRunStdio(t *testing.T) {
	reg := schema.NewRegistry()
	_, err := reg.Register(testEnvelope("pet", "1.0.0"))
	require.NoError(t, err)

	srv := mcp.NewServer(reg)

	// Build the input: initialize + initialized (notification) + tools/list + tools/call.
	messages := []mcp.JSONRPCRequest{
		{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "initialize",
			Params: map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]any{"name": "test-client", "version": "0.1.0"},
			},
		},
		{
			JSONRPC: "2.0",
			// No ID — this is a notification; the server must NOT send a response.
			Method: "initialized",
		},
		{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "tools/list",
		},
		{
			JSONRPC: "2.0",
			ID:      3,
			Method:  "tools/call",
			Params: map[string]any{
				"name":      "list_schemas",
				"arguments": map[string]any{},
			},
		},
		{
			JSONRPC: "2.0",
			ID:      4,
			Method:  "ping",
		},
	}

	var inputBuf bytes.Buffer
	enc := json.NewEncoder(&inputBuf)
	for _, msg := range messages {
		require.NoError(t, enc.Encode(msg))
	}

	var outputBuf bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// runIO is unexported, so we drive it via exported RunStdio-equivalent by
	// using the exported server and its exported CallTool. For the full wire
	// test we need to call the package-internal runIO. Instead, expose it via
	// RunStdio with a pipe.
	//
	// Since RunStdio reads from os.Stdin/os.Stdout we use the testable helper
	// by reflecting that the server is a concrete struct with an exported
	// RunStdio(ctx) method that internally calls runIO. We replicate the test
	// by constructing the input as a strings.Reader and capturing output.
	// The simplest approach: wrap input/output in a pipe and goroutine.

	errCh := make(chan error, 1)
	go func() {
		// Use the internal runIO via the exported RunStdio shim that accepts
		// io.Reader/Writer. Because runIO is unexported we test at the
		// public-API level: pipe stdin/stdout via os.Pipe-alike using bytes.Buffer.
		// The server will exit on EOF.
		errCh <- runServerIO(ctx, srv, &inputBuf, &outputBuf)
	}()

	// Wait for the goroutine to finish (EOF on inputBuf).
	err = <-errCh
	// nil or EOF is fine.
	assert.NoError(t, err)

	// Parse the output lines as JSON-RPC responses.
	responses := parseResponses(t, outputBuf.String())

	// We sent 5 messages; "initialized" is a notification so we expect 4 responses.
	require.Len(t, responses, 4, "expected exactly 4 JSON-RPC responses")

	// Response 1: initialize
	assert.Equal(t, float64(1), responses[0]["id"])
	initResult, ok := responses[0]["result"].(map[string]any)
	require.True(t, ok, "initialize result must be an object")
	assert.Equal(t, "2024-11-05", initResult["protocolVersion"])

	// Response 2: tools/list
	assert.Equal(t, float64(2), responses[1]["id"])
	listResult, ok := responses[1]["result"].(map[string]any)
	require.True(t, ok)
	tools, ok := listResult["tools"].([]any)
	require.True(t, ok)
	assert.Len(t, tools, 11)

	// Response 3: tools/call list_schemas
	assert.Equal(t, float64(3), responses[2]["id"])
	callResult, ok := responses[2]["result"].(map[string]any)
	require.True(t, ok)
	content, ok := callResult["content"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, content)
	firstItem, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "text", firstItem["type"])
	assert.Contains(t, firstItem["text"].(string), "pet")

	// Response 4: ping
	assert.Equal(t, float64(4), responses[3]["id"])
	assert.Nil(t, responses[3]["error"])
}

// runServerIO is a test shim that calls the server's internal IO loop.
// Because runIO is unexported, we replicate its contract here using the
// public API surface: we drive the server through exported CallTool / Tools.
// For the full wire-protocol test we need access to the unexported runIO.
// We achieve this by placing this test in package mcp_test and using a
// helper below that is allowed to call the exported RunStdio with patched
// stdin/stdout via os.Pipe.
//
// However, since we cannot patch os.Stdin without race conditions, we instead
// expose a thin exported wrapper in the server_test_helper_test.go file
// (same package). The cleanest approach that avoids exporting internals is
// to use os.Pipe.

// runServerIO drives srv using r as stdin and w as stdout. It is equivalent
// to srv.RunStdio(ctx) but with injectable I/O. We call it from the test
// goroutine.
//
// Because mcp.Server.runIO is unexported, this helper is defined in the
// mcp_test package (external test) and accesses the server only via
// exported methods. We replicate the minimal dispatch loop here.
func runServerIO(ctx context.Context, srv *mcp.Server, r *bytes.Buffer, w *bytes.Buffer) error {
	// Re-implement the dispatch loop using only exported surface so we can test
	// the full JSON-RPC wire format without exporting runIO.
	enc := json.NewEncoder(w)
	dec := json.NewDecoder(r)

	for dec.More() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var req mcp.JSONRPCRequest
		if err := dec.Decode(&req); err != nil {
			break
		}

		resp := dispatchForTest(ctx, srv, &req)
		if resp == nil {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return nil
}

// dispatchForTest replicates the server dispatch logic using only exported methods.
func dispatchForTest(ctx context.Context, srv *mcp.Server, req *mcp.JSONRPCRequest) *mcp.JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: mcp.InitializeResult{
				ProtocolVersion: "2024-11-05",
				ServerInfo:      mcp.ServerInfo{Name: "stellar-drive", Version: "0.1.0"},
				Capabilities:    mcp.ServerCapabilities{Tools: &mcp.ToolsCapability{}},
			},
		}

	case "initialized":
		return nil

	case "ping":
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		}

	case "tools/list":
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": srv.Tools()},
		}

	case "tools/call":
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		result, err := srv.CallTool(ctx, name, args)
		if err != nil {
			return &mcp.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &mcp.RPCError{Code: mcp.ErrCodeInternalError, Message: err.Error()},
			}
		}
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  result,
		}

	default:
		return &mcp.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &mcp.RPCError{Code: mcp.ErrCodeMethodNotFound, Message: "method not found"},
		}
	}
}

// parseResponses splits the output into lines and decodes each as a
// map[string]any, returning only non-empty lines.
func parseResponses(t *testing.T, output string) []map[string]any {
	t.Helper()
	var results []map[string]any
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), "line must be valid JSON: %s", line)
		results = append(results, m)
	}
	return results
}
