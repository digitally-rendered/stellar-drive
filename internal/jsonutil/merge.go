// Package jsonutil provides utilities for working with JSON documents
// represented as generic Go maps and slices.
package jsonutil

// DeepMerge merges patch into base and returns the result. The original maps
// are never mutated — a new map is always allocated for the result.
//
// Merge rules:
//   - Scalar values (string, number, bool) in patch overwrite base.
//   - If both base and patch hold a map[string]any for the same key, the maps
//     are merged recursively.
//   - A nil value in patch explicitly deletes the key from the result.
//   - Arrays/slices are replaced wholesale — patch wins, no element merging.
//
// Example:
//
//	base  := map[string]any{"a": 1, "b": map[string]any{"x": 10, "y": 20}}
//	patch := map[string]any{"b": map[string]any{"y": 99}, "c": 3}
//	// result: {"a": 1, "b": {"x": 10, "y": 99}, "c": 3}
func DeepMerge(base, patch map[string]any) map[string]any {
	result := make(map[string]any, len(base))

	// Copy all base keys into result first.
	for k, v := range base {
		result[k] = v
	}

	// Apply patch on top.
	for k, pv := range patch {
		if pv == nil {
			// Explicit nil in patch deletes the key.
			delete(result, k)
			continue
		}

		patchMap, patchIsMap := pv.(map[string]any)
		if patchIsMap {
			// If base also has a map at this key, recurse.
			if bv, exists := result[k]; exists {
				if baseMap, baseIsMap := bv.(map[string]any); baseIsMap {
					result[k] = DeepMerge(baseMap, patchMap)
					continue
				}
			}
			// Base is missing or non-map: deep-copy the patch map so we never
			// share mutable state with the caller.
			result[k] = DeepMerge(map[string]any{}, patchMap)
			continue
		}

		// Scalar or array — patch wins outright.
		result[k] = pv
	}

	return result
}
