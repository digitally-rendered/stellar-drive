package schema

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/mod/semver"
)

// Registry loads, stores, and indexes schema envelopes.
// Thread-safe for concurrent access.
type Registry struct {
	mu        sync.RWMutex
	schemas   map[string]map[string]*SchemaDefinition // name -> version -> definition
	latest    map[string]string                        // name -> latest version
	envelopes map[string]map[string]*SchemaEnvelope    // name -> version -> envelope
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		schemas:   make(map[string]map[string]*SchemaDefinition),
		latest:    make(map[string]string),
		envelopes: make(map[string]map[string]*SchemaEnvelope),
	}
}

// Register adds a schema envelope to the registry.
// Parses the envelope, extracts field definitions, and updates the latest pointer
// when the new version is greater than the current latest.
func (r *Registry) Register(envelope *SchemaEnvelope) (*SchemaDefinition, error) {
	if err := ValidateEnvelope(envelope); err != nil {
		return nil, fmt.Errorf("register schema %q: %w", envelope.Name, err)
	}

	def, err := r.parseEnvelope(envelope)
	if err != nil {
		return nil, fmt.Errorf("register schema %q: parse: %w", envelope.Name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.schemas[envelope.Name] == nil {
		r.schemas[envelope.Name] = make(map[string]*SchemaDefinition)
	}
	if r.envelopes[envelope.Name] == nil {
		r.envelopes[envelope.Name] = make(map[string]*SchemaEnvelope)
	}

	r.schemas[envelope.Name][envelope.Version] = def
	r.envelopes[envelope.Name][envelope.Version] = envelope

	// Update the latest pointer when the incoming version is newer.
	current, exists := r.latest[envelope.Name]
	if !exists || isNewerVersion(envelope.Version, current) {
		r.latest[envelope.Name] = envelope.Version
	}

	return def, nil
}

// Get returns the schema definition for a name and version.
// If version is empty, returns the latest registered version.
func (r *Registry) Get(name string, version string) (*SchemaDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.schemas[name]
	if !ok {
		return nil, fmt.Errorf("schema %q: not found", name)
	}

	v := version
	if v == "" {
		v = r.latest[name]
	}

	def, ok := versions[v]
	if !ok {
		return nil, fmt.Errorf("schema %q version %q: not found", name, v)
	}
	return def, nil
}

// GetEnvelope returns the raw schema envelope for a name and version.
// If version is empty, returns the latest version.
func (r *Registry) GetEnvelope(name string, version string) (*SchemaEnvelope, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, ok := r.envelopes[name]
	if !ok {
		return nil, fmt.Errorf("schema %q: not found", name)
	}

	v := version
	if v == "" {
		v = r.latest[name]
	}

	env, ok := versions[v]
	if !ok {
		return nil, fmt.Errorf("schema %q version %q: not found", name, v)
	}
	return env, nil
}

// List returns the latest SchemaDefinition for every registered schema name.
func (r *Registry) List() []*SchemaDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*SchemaDefinition, 0, len(r.schemas))
	for name, latestVersion := range r.latest {
		if def, ok := r.schemas[name][latestVersion]; ok {
			out = append(out, def)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ListAll returns all versions of all registered schemas.
func (r *Registry) ListAll() []*SchemaDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*SchemaDefinition
	for _, versions := range r.schemas {
		for _, def := range versions {
			out = append(out, def)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

// Has returns true if at least one version of the named schema exists.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.schemas[name]
	return ok
}

// Remove deletes all versions of the named schema from the registry.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.schemas, name)
	delete(r.envelopes, name)
	delete(r.latest, name)
}

// Names returns all registered schema names in lexicographic order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.schemas))
	for name := range r.schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseEnvelope converts a SchemaEnvelope into a SchemaDefinition by extracting
// field definitions from the JSON Schema properties block.
func (r *Registry) parseEnvelope(envelope *SchemaEnvelope) (*SchemaDefinition, error) {
	raw := envelope.Schema
	if raw == nil {
		raw = map[string]any{}
	}

	var requiredFields []string
	if req, ok := raw["required"]; ok {
		switch v := req.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					requiredFields = append(requiredFields, s)
				}
			}
		case []string:
			requiredFields = v
		}
	}

	var fields []FieldDefinition
	if props, ok := raw["properties"]; ok {
		if propsMap, ok := props.(map[string]any); ok {
			fields = extractFields(propsMap, requiredFields)
		}
	}

	def := &SchemaDefinition{
		Name:           envelope.Name,
		Version:        envelope.Version,
		Description:    envelope.Description,
		RawSchema:      raw,
		Fields:         fields,
		RequiredFields: requiredFields,
		Indexes:        envelope.Indexes,
		Storage:        envelope.Storage,
		Collection:     envelope.Collection,
		Extensions:     envelope.Extensions,
	}
	return def, nil
}

// extractFields parses JSON Schema properties into FieldDefinitions.
func extractFields(properties map[string]any, required []string) []FieldDefinition {
	requiredSet := make(map[string]bool, len(required))
	for _, r := range required {
		requiredSet[r] = true
	}

	fields := make([]FieldDefinition, 0, len(properties))
	for name, raw := range properties {
		propMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fd := buildFieldDef(name, propMap, requiredSet[name])
		fields = append(fields, fd)
	}

	// Deterministic ordering by field name.
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

// buildFieldDef constructs a FieldDefinition from a single property map.
func buildFieldDef(name string, prop map[string]any, required bool) FieldDefinition {
	fd := FieldDefinition{
		Name:     name,
		Required: required,
	}

	if ref, ok := prop["$ref"].(string); ok {
		fd.Ref = ref
		return fd
	}

	if t, ok := prop["type"].(string); ok {
		fd.JSONType = t
	}
	if f, ok := prop["format"].(string); ok {
		fd.Format = f
	}

	fd.GoType = resolveGoType(fd.JSONType, fd.Format)

	if d, ok := prop["default"]; ok {
		fd.Default = d
	}

	if enum, ok := prop["enum"].([]any); ok {
		fd.Enum = enum
	}

	// Nested object properties.
	if fd.JSONType == "object" {
		if nestedProps, ok := prop["properties"].(map[string]any); ok {
			var nestedRequired []string
			if req, ok := prop["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						nestedRequired = append(nestedRequired, s)
					}
				}
			}
			fd.Properties = extractFields(nestedProps, nestedRequired)
		}
	}

	// Array items.
	if fd.JSONType == "array" {
		if items, ok := prop["items"].(map[string]any); ok {
			itemFD := buildFieldDef("", items, false)
			fd.Items = &itemFD
			if itemFD.GoType != "" {
				fd.GoType = "[]" + itemFD.GoType
			} else {
				fd.GoType = "[]any"
			}
		}
	}

	// x-stellar-* extensions.
	for k, v := range prop {
		if strings.HasPrefix(k, "x-stellar-") || strings.HasPrefix(k, "x-") {
			if fd.Extensions == nil {
				fd.Extensions = make(map[string]any)
			}
			fd.Extensions[k] = v
		}
	}

	return fd
}

// resolveGoType maps a JSON Schema type+format combination to a Go type string.
func resolveGoType(jsonType, format string) string {
	switch jsonType {
	case "string":
		switch format {
		case "date-time":
			return "time.Time"
		case "email", "uuid", "uri", "hostname", "ipv4", "ipv6":
			return "string"
		default:
			return "string"
		}
	case "number":
		return "float64"
	case "integer":
		return "int64"
	case "boolean":
		return "bool"
	case "array":
		return "[]any"
	case "object":
		return "map[string]any"
	case "null":
		return "any"
	default:
		return "any"
	}
}

// isNewerVersion reports whether candidate is a strictly newer version than current.
// Tries semver first; falls back to lexicographic ordering.
func isNewerVersion(candidate, current string) bool {
	c := normalizeSemver(candidate)
	cur := normalizeSemver(current)
	if semver.IsValid(c) && semver.IsValid(cur) {
		return semver.Compare(c, cur) > 0
	}
	// Fallback: lexicographic comparison.
	return candidate > current
}

// normalizeSemver prepends "v" if absent so golang.org/x/mod/semver can parse it.
func normalizeSemver(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
