package port

import (
	"context"

	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
)

// AuditStore is the secondary (driven) port for persisting audit trail entries.
type AuditStore interface {
	// Record persists a single audit entry.
	Record(ctx context.Context, entry *model.AuditEntry) error

	// GetHistory returns audit entries for the given entity ID, ordered by
	// timestamp descending.
	GetHistory(ctx context.Context, entityID string, opts ...AuditQueryOption) ([]*model.AuditEntry, error)

	// GetUserActivity returns audit entries for the given user ID, ordered by
	// timestamp descending.
	GetUserActivity(ctx context.Context, userID string, opts ...AuditQueryOption) ([]*model.AuditEntry, error)
}

// AuditQuery holds query options for audit trail lookups.
type AuditQuery struct {
	SchemaName string
	Limit      int
}

// AuditQueryOption configures an AuditQuery.
type AuditQueryOption func(*AuditQuery)

// WithAuditSchemaName filters audit entries to a specific schema.
func WithAuditSchemaName(name string) AuditQueryOption {
	return func(q *AuditQuery) {
		q.SchemaName = name
	}
}

// WithAuditLimit sets the maximum number of entries to return.
func WithAuditLimit(n int) AuditQueryOption {
	return func(q *AuditQuery) {
		q.Limit = n
	}
}

// BuildAuditQuery applies options and returns the resulting AuditQuery.
func BuildAuditQuery(opts ...AuditQueryOption) AuditQuery {
	q := AuditQuery{Limit: 100}
	for _, opt := range opts {
		opt(&q)
	}
	return q
}
