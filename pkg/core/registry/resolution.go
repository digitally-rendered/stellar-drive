package registry

import "sort"

// matchScore returns a specificity score for a registered Scope against the
// incoming request's version and channel strings. A higher score means a more
// specific (preferred) match. Returns -1 when the scope does not match.
//
// Specificity order (highest to lowest):
//
//	4 — version + channel both match
//	3 — channel matches, scope has no version constraint
//	2 — version matches, scope has no channel constraint
//	1 — universal scope (both fields empty)
//	-1 — scope has a constraint that does not match; reject
func matchScore(registered Scope, requestVersion, requestChannel string) int {
	versionMatch := registered.Version == "" || registered.Version == requestVersion
	channelMatch := registered.Channel == "" || registered.Channel == requestChannel

	if !versionMatch || !channelMatch {
		return -1
	}

	switch {
	case registered.Version != "" && registered.Channel != "":
		return 4
	case registered.Version == "" && registered.Channel != "":
		return 3
	case registered.Version != "" && registered.Channel == "":
		return 2
	default:
		return 1
	}
}

// ---------------------------------------------------------------------------
// Scoped wrapper types
// ---------------------------------------------------------------------------

// scopedHandler pairs a HandlerFunc with its Scope.
type scopedHandler struct {
	scope   Scope
	handler HandlerFunc
}

// scopedGuard pairs a GuardFunc with its Scope.
type scopedGuard struct {
	scope Scope
	guard GuardFunc
}

// scopedValidator pairs a ValidatorFunc with its Scope.
type scopedValidator struct {
	scope     Scope
	validator ValidatorFunc
}

// scopedTransform pairs a TransformFunc with its Scope.
type scopedTransform struct {
	scope     Scope
	transform TransformFunc
}

// ---------------------------------------------------------------------------
// Generic sorting helpers
// ---------------------------------------------------------------------------

// sortHandlersBySpecificity sorts a slice of scopedHandlers by decreasing
// matchScore. Entries that do not match are removed. The version and channel
// of the incoming request are used to compute scores.
func sortHandlersBySpecificity(entries []scopedHandler, version, channel string) []scopedHandler {
	type scored struct {
		entry scopedHandler
		score int
	}

	var matching []scored
	for _, e := range entries {
		if s := matchScore(e.scope, version, channel); s >= 0 {
			matching = append(matching, scored{e, s})
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].score > matching[j].score
	})

	out := make([]scopedHandler, len(matching))
	for i, m := range matching {
		out[i] = m.entry
	}
	return out
}

// filterGuards returns matching guards ordered by decreasing specificity.
func filterGuards(entries []scopedGuard, version, channel string) []GuardFunc {
	type scored struct {
		entry scopedGuard
		score int
	}

	var matching []scored
	for _, e := range entries {
		if s := matchScore(e.scope, version, channel); s >= 0 {
			matching = append(matching, scored{e, s})
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].score > matching[j].score
	})

	out := make([]GuardFunc, len(matching))
	for i, m := range matching {
		out[i] = m.entry.guard
	}
	return out
}

// filterValidators returns matching validators ordered by decreasing specificity.
func filterValidators(entries []scopedValidator, version, channel string) []ValidatorFunc {
	type scored struct {
		entry scopedValidator
		score int
	}

	var matching []scored
	for _, e := range entries {
		if s := matchScore(e.scope, version, channel); s >= 0 {
			matching = append(matching, scored{e, s})
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].score > matching[j].score
	})

	out := make([]ValidatorFunc, len(matching))
	for i, m := range matching {
		out[i] = m.entry.validator
	}
	return out
}

// filterTransforms returns matching transforms ordered by decreasing specificity.
func filterTransforms(entries []scopedTransform, version, channel string) []TransformFunc {
	type scored struct {
		entry scopedTransform
		score int
	}

	var matching []scored
	for _, e := range entries {
		if s := matchScore(e.scope, version, channel); s >= 0 {
			matching = append(matching, scored{e, s})
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].score > matching[j].score
	})

	out := make([]TransformFunc, len(matching))
	for i, m := range matching {
		out[i] = m.entry.transform
	}
	return out
}
