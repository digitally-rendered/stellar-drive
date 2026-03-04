package schema

import (
	"fmt"
	"strings"
)

// ResolveRefs resolves $ref pointers within a JSON Schema map.
// Supports:
//   - Internal references of the form #/definitions/Foo
//   - Cross-schema references of the form SchemaName#/definitions/Foo
//     where SchemaName matches a registered schema's Name field.
//
// The returned map is a deep copy with all $ref nodes replaced by the
// referenced schema fragment. Circular references are detected and cause
// an error.
func ResolveRefs(schema map[string]any, registry *Registry) (map[string]any, error) {
	if schema == nil {
		return nil, nil
	}
	visited := make(map[string]bool)
	out, err := resolveNode(schema, schema, registry, visited, 0)
	if err != nil {
		return nil, err
	}
	if m, ok := out.(map[string]any); ok {
		return m, nil
	}
	return nil, fmt.Errorf("resolve refs: unexpected root type %T", out)
}

const maxRefDepth = 32

// resolveNode recursively walks a JSON Schema node and replaces $ref entries.
func resolveNode(
	node any,
	root map[string]any,
	registry *Registry,
	visited map[string]bool,
	depth int,
) (any, error) {
	if depth > maxRefDepth {
		return nil, fmt.Errorf("resolve refs: maximum recursion depth %d exceeded (possible circular $ref)", maxRefDepth)
	}

	switch v := node.(type) {
	case map[string]any:
		// If this object has a $ref, resolve it.
		if ref, ok := v["$ref"].(string); ok {
			return resolveRef(ref, root, registry, visited, depth+1)
		}

		// Otherwise recurse into every value.
		out := make(map[string]any, len(v))
		for key, val := range v {
			resolved, err := resolveNode(val, root, registry, visited, depth+1)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil

	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			resolved, err := resolveNode(item, root, registry, visited, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil

	default:
		return v, nil
	}
}

// resolveRef resolves a single $ref string.
func resolveRef(
	ref string,
	root map[string]any,
	registry *Registry,
	visited map[string]bool,
	depth int,
) (any, error) {
	if visited[ref] {
		return nil, fmt.Errorf("resolve refs: circular reference detected at %q", ref)
	}
	visited[ref] = true
	defer func() { delete(visited, ref) }()

	var (
		searchRoot map[string]any
		pointer    string
	)

	if strings.HasPrefix(ref, "#") {
		// Internal reference: #/definitions/Foo
		searchRoot = root
		pointer = ref[1:] // strip leading "#"
	} else if idx := strings.Index(ref, "#"); idx >= 0 {
		// Cross-schema reference: SchemaName#/definitions/Foo or SchemaName#
		schemaName := ref[:idx]
		pointer = ref[idx+1:]

		env, err := registry.GetEnvelope(schemaName, "")
		if err != nil {
			return nil, fmt.Errorf("resolve ref %q: cross-schema lookup: %w", ref, err)
		}
		searchRoot = env.Schema
	} else {
		// Plain schema name with no pointer — resolve to the schema root.
		env, err := registry.GetEnvelope(ref, "")
		if err != nil {
			return nil, fmt.Errorf("resolve ref %q: schema lookup: %w", ref, err)
		}
		resolved, err := resolveNode(env.Schema, env.Schema, registry, visited, depth)
		if err != nil {
			return nil, fmt.Errorf("resolve ref %q: %w", ref, err)
		}
		return resolved, nil
	}

	// Walk the JSON pointer (e.g. /definitions/Foo).
	target, err := jsonPointerLookup(searchRoot, pointer)
	if err != nil {
		return nil, fmt.Errorf("resolve ref %q: %w", ref, err)
	}

	// Recurse in case the resolved fragment itself contains $refs.
	return resolveNode(target, searchRoot, registry, visited, depth)
}

// jsonPointerLookup navigates a JSON Pointer string (RFC 6901) within a schema.
// An empty pointer returns the root.
func jsonPointerLookup(root map[string]any, pointer string) (any, error) {
	if pointer == "" || pointer == "/" {
		return root, nil
	}

	// Strip leading slash.
	pointer = strings.TrimPrefix(pointer, "/")
	parts := strings.Split(pointer, "/")

	var current any = root
	for _, part := range parts {
		// Unescape RFC 6901 tokens.
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")

		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("json pointer: cannot index into %T at segment %q", current, part)
		}
		val, exists := m[part]
		if !exists {
			return nil, fmt.Errorf("json pointer: key %q not found", part)
		}
		current = val
	}
	return current, nil
}
