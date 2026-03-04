package service

// VersionInfo holds version metadata for a document. It is a lightweight
// value type used by callers that need to track or advance version state
// without loading the full model.Document.
type VersionInfo struct {
	EntityID      string
	RecordVersion int
	SchemaVersion string
}

// NextVersion returns a new VersionInfo with RecordVersion incremented by one.
// The EntityID and SchemaVersion are carried over unchanged. The original
// VersionInfo is not mutated.
func NextVersion(current *VersionInfo) *VersionInfo {
	return &VersionInfo{
		EntityID:      current.EntityID,
		RecordVersion: current.RecordVersion + 1,
		SchemaVersion: current.SchemaVersion,
	}
}
