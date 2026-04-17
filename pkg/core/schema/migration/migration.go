// Package migration provides a read-time schema-version migration registry.
//
// Stellar-drive persists documents with the schema version they were written
// at (stored on Document.SchemaVersion). When a schema evolves — a field is
// renamed, split, defaulted, or dropped — old documents on disk still carry
// the old shape. Rather than forcing a bulk rewrite of every record on every
// schema change, this package lets consumers register per-(fromVersion,
// toVersion) migration functions that run on read and transform the document
// payload into the current shape before it leaves the repository.
//
// Writes always happen at the current schema version; the write path is
// unchanged. A separate "compact" operation can be built on top of this
// registry to rewrite old documents at rest.
package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/mod/semver"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
)

// Func transforms a document's Data map from one schema version to the next.
// Implementations should be pure: mutate the returned map, not the input, and
// avoid external side-effects so migrations remain deterministic and safely
// retryable.
type Func func(ctx context.Context, data map[string]any) (map[string]any, error)

// step describes a single migration edge: fromVersion -> toVersion.
type step struct {
	from string
	to   string
	fn   Func
}

// Registry stores migration steps keyed by schema name. For each schema the
// registry can resolve a path of steps from any known source version to any
// later target version. A Registry is safe for concurrent use.
type Registry struct {
	mu    sync.RWMutex
	steps map[string][]step // schemaName -> ordered list of edges
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{steps: make(map[string][]step)}
}

// Register adds a migration edge for schemaName that transforms data from
// fromVersion to toVersion. fromVersion and toVersion must both be non-empty
// and differ; registering the same edge twice replaces the previous entry.
func (r *Registry) Register(schemaName, fromVersion, toVersion string, fn Func) error {
	if schemaName == "" {
		return fmt.Errorf("migration: schema name is required")
	}
	if fromVersion == "" || toVersion == "" {
		return fmt.Errorf("migration: from and to versions are required")
	}
	if fromVersion == toVersion {
		return fmt.Errorf("migration: from and to must differ (got %q)", fromVersion)
	}
	if fn == nil {
		return fmt.Errorf("migration: function is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	edges := r.steps[schemaName]
	for i, e := range edges {
		if e.from == fromVersion && e.to == toVersion {
			edges[i].fn = fn
			r.steps[schemaName] = edges
			return nil
		}
	}
	r.steps[schemaName] = append(edges, step{from: fromVersion, to: toVersion, fn: fn})
	return nil
}

// Has reports whether at least one migration is registered for schemaName.
func (r *Registry) Has(schemaName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.steps[schemaName]) > 0
}

// Chain returns the ordered list of migration edges that transform a document
// from fromVersion to toVersion. A zero-length chain is returned when
// fromVersion == toVersion. An error is returned if the two versions are not
// connected by registered edges.
//
// Resolution strategy: at each step pick the edge whose `from` matches the
// current version and whose `to` is closest to (but not past) toVersion, using
// semver ordering when both versions are valid semver and lexicographic order
// otherwise. This lets authors register small steps (1.0.0 -> 1.1.0 -> 2.0.0)
// or larger jumps (1.0.0 -> 2.0.0) and have the registry pick the right path.
func (r *Registry) Chain(schemaName, fromVersion, toVersion string) ([]Func, error) {
	if fromVersion == toVersion {
		return nil, nil
	}

	r.mu.RLock()
	edges := append([]step(nil), r.steps[schemaName]...)
	r.mu.RUnlock()

	if len(edges) == 0 {
		return nil, fmt.Errorf("migration: no migrations registered for schema %q", schemaName)
	}

	// Build adjacency by `from`.
	byFrom := make(map[string][]step, len(edges))
	for _, e := range edges {
		byFrom[e.from] = append(byFrom[e.from], e)
	}
	// Prefer edges closer to (but not past) toVersion.
	for k := range byFrom {
		sort.SliceStable(byFrom[k], func(i, j int) bool {
			return cmpVersion(byFrom[k][i].to, byFrom[k][j].to) > 0
		})
	}

	current := fromVersion
	var chain []Func
	visited := make(map[string]bool, len(edges))
	for current != toVersion {
		if visited[current] {
			return nil, fmt.Errorf("migration: cycle detected at version %q for schema %q", current, schemaName)
		}
		visited[current] = true

		candidates := byFrom[current]
		if len(candidates) == 0 {
			return nil, fmt.Errorf("migration: no edge from %q to %q for schema %q", current, toVersion, schemaName)
		}

		var picked *step
		for i := range candidates {
			if cmpVersion(candidates[i].to, toVersion) <= 0 {
				picked = &candidates[i]
				break
			}
		}
		if picked == nil {
			return nil, fmt.Errorf("migration: no edge from %q that does not overshoot %q for schema %q", current, toVersion, schemaName)
		}

		chain = append(chain, picked.fn)
		current = picked.to
	}

	return chain, nil
}

// Apply runs every registered migration needed to bring a document from its
// recorded schema version up to targetVersion. On success the document's Data
// map is replaced with the migrated output and SchemaVersion is updated to
// targetVersion. Documents already at targetVersion are returned unchanged.
//
// Apply is a no-op when no migrations are registered for the schema — this
// keeps the decorator safe to install globally even when only a few schemas
// evolve.
func (r *Registry) Apply(ctx context.Context, doc *model.Document, targetVersion string) error {
	if doc == nil || targetVersion == "" {
		return nil
	}
	if doc.SchemaVersion == targetVersion {
		return nil
	}
	if !r.Has(doc.SchemaName) {
		return nil
	}

	chain, err := r.Chain(doc.SchemaName, doc.SchemaVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("migration: build chain for %s (%s -> %s): %w",
			doc.SchemaName, doc.SchemaVersion, targetVersion, err)
	}

	data := doc.Data
	for i, fn := range chain {
		next, err := fn(ctx, data)
		if err != nil {
			return fmt.Errorf("migration: schema %s step %d: %w", doc.SchemaName, i, err)
		}
		if next != nil {
			data = next
		}
	}

	doc.Data = data
	doc.SchemaVersion = targetVersion
	return nil
}

// cmpVersion compares two version strings using semver when both are valid
// semver, falling back to lexicographic order otherwise. Returns -1, 0, or 1.
func cmpVersion(a, b string) int {
	an := normalizeSemver(a)
	bn := normalizeSemver(b)
	if semver.IsValid(an) && semver.IsValid(bn) {
		return semver.Compare(an, bn)
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func normalizeSemver(v string) string {
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
