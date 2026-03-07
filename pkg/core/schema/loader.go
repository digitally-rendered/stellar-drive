package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadFromDir reads all *.schema.json files from dir, parses each as a
// SchemaEnvelope, and registers them with the provided registry.
// Returns the first error encountered; partial registration may have occurred.
func LoadFromDir(registry *Registry, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("load schemas from dir %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".schema.json") {
			continue
		}

		path := filepath.Join(dir, name)
		envelope, err := LoadFromFile(path)
		if err != nil {
			return fmt.Errorf("load schema file %q: %w", path, err)
		}

		if _, err := registry.Register(envelope); err != nil {
			return fmt.Errorf("register schema from %q: %w", path, err)
		}
	}
	return nil
}

// LoadFromFile reads a single *.schema.json file and decodes it as a SchemaEnvelope.
func LoadFromFile(path string) (*SchemaEnvelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open schema file %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var envelope SchemaEnvelope
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&envelope); err != nil {
		// Retry without strict unknown-field checking so extra keys in the
		// schema body do not cause spurious errors.
		if _, seekErr := f.Seek(0, 0); seekErr != nil {
			return nil, fmt.Errorf("decode schema file %q: %w", path, err)
		}
		var envelope2 SchemaEnvelope
		if err2 := json.NewDecoder(f).Decode(&envelope2); err2 != nil {
			return nil, fmt.Errorf("decode schema file %q: %w", path, err2)
		}
		return &envelope2, nil
	}
	return &envelope, nil
}
