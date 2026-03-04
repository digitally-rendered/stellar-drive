package sql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digitally-rendered/stellar-drive/internal/jsonutil"
	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
	"github.com/google/uuid"
)

// SQLRepository implements port.Repository with append-only versioned
// persistence backed by any database/sql-compatible driver. Every mutation
// inserts a new row; no existing row is ever modified or deleted in place.
type SQLRepository struct {
	conn    *Connection
	dialect Dialect
}

// NewRepository returns a SQLRepository that uses the given Connection and
// Dialect.
func NewRepository(conn *Connection, dialect Dialect) *SQLRepository {
	return &SQLRepository{conn: conn, dialect: dialect}
}

// generateETag produces a SHA-256 hex digest of the JSON-encoded data map.
func generateETag(data map[string]any) string {
	b, _ := json.Marshal(data)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// marshalData serialises a map to a JSON string for storage in the data column.
func marshalData(data map[string]any) (string, error) {
	if data == nil {
		return "{}", nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("sql: marshal data: %w", err)
	}
	return string(b), nil
}

// unmarshalData deserialises a JSON string from the data column into a map.
func unmarshalData(s string) (map[string]any, error) {
	if s == "" || s == "{}" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("sql: unmarshal data: %w", err)
	}
	return m, nil
}

// parseTime parses a stored time string. Stored as RFC3339Nano; empty string
// returns the zero time.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// parseNullTime parses an optional time string into a *time.Time pointer.
func parseNullTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil
	}
	return &t
}

// scanRow scans a single row from a versioned table SELECT into a *model.Document.
// The expected column order is:
//
//	entity_id, schema_name, record_version, schema_version, data,
//	created_at, updated_at, deleted_at, created_by, updated_by, etag
//
// Note: the surrogate "id" column is not part of the document identity and is
// not scanned. entity_id is used as the stable public identifier.
func scanRow(row interface {
	Scan(dest ...any) error
}) (*model.Document, error) {
	var (
		entityID      string
		schemaName    string
		recordVersion int
		schemaVersion string
		dataStr       string
		createdAtStr  string
		updatedAtStr  string
		deletedAtStr  sql.NullString
		createdBy     string
		updatedBy     string
		etag          string
	)

	if err := row.Scan(
		&entityID,
		&schemaName,
		&recordVersion,
		&schemaVersion,
		&dataStr,
		&createdAtStr,
		&updatedAtStr,
		&deletedAtStr,
		&createdBy,
		&updatedBy,
		&etag,
	); err != nil {
		return nil, err
	}

	data, err := unmarshalData(dataStr)
	if err != nil {
		return nil, err
	}

	doc := &model.Document{
		ID:            entityID, // expose entity_id as the document's public ID
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: recordVersion,
		SchemaVersion: schemaVersion,
		Data:          data,
		CreatedAt:     parseTime(createdAtStr),
		UpdatedAt:     parseTime(updatedAtStr),
		DeletedAt:     parseNullTime(deletedAtStr),
		CreatedBy:     createdBy,
		UpdatedBy:     updatedBy,
		ETag:          etag,
	}

	return doc, nil
}

// selectCols is the ordered list of columns used in every SELECT.
const selectCols = `entity_id, schema_name, record_version, schema_version, data,
    created_at, updated_at, deleted_at, created_by, updated_by, etag`

// Create persists a new document and returns the stored Document.
func (r *SQLRepository) Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error) {
	if data == nil {
		data = map[string]any{}
	}

	now := time.Now().UTC()
	entityID := uuid.New().String()
	etag := generateETag(data)

	dataStr, err := marshalData(data)
	if err != nil {
		return nil, coreerrors.Internal("failed to marshal document data", err)
	}

	d := r.dialect
	tbl := d.QuoteIdentifier(tableName(schemaName))

	cols := []string{
		"entity_id", "schema_name", "record_version", "schema_version",
		"data", "created_at", "updated_at", "created_by", "updated_by", "etag",
	}
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = d.Placeholder(i + 1)
	}

	q := fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s)`,
		tbl,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	nowStr := now.Format(time.RFC3339Nano)

	_, err = r.conn.DB().ExecContext(ctx, q,
		entityID,
		schemaName,
		1, // record_version
		"",
		dataStr,
		nowStr,
		nowStr,
		"", // created_by
		"", // updated_by
		etag,
	)
	if err != nil {
		return nil, coreerrors.Internal("failed to create document", err)
	}

	return &model.Document{
		ID:            entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: 1,
		Data:          data,
		CreatedAt:     now,
		UpdatedAt:     now,
		ETag:          etag,
	}, nil
}

// FindByID retrieves the current (non-deleted) document for entityID by
// fetching the row with the highest record_version. Returns NotFound when the
// entity does not exist or its latest version is a tombstone.
func (r *SQLRepository) FindByID(ctx context.Context, schemaName, entityID string) (*model.Document, error) {
	d := r.dialect
	tbl := d.QuoteIdentifier(tableName(schemaName))

	q := fmt.Sprintf(
		`SELECT %s FROM %s
         WHERE entity_id = %s
         ORDER BY record_version DESC
         LIMIT 1`,
		selectCols, tbl, d.Placeholder(1),
	)

	row := r.conn.DB().QueryRowContext(ctx, q, entityID)
	doc, err := scanRow(row)
	if err == sql.ErrNoRows {
		return nil, coreerrors.NotFound(schemaName, entityID)
	}
	if err != nil {
		return nil, coreerrors.Internal("failed to find document", err)
	}

	if doc.IsTombstone() {
		return nil, coreerrors.NotFound(schemaName, entityID)
	}

	return doc, nil
}

// List executes a structured query using a subquery that resolves the latest
// version per entity, filters tombstones, applies user filters and sort, and
// returns a paginated ListResult.
func (r *SQLRepository) List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error) {
	d := r.dialect

	// The base subquery resolves the latest version per entity_id.
	baseSubq := latestVersionSubquery(schemaName, d)

	// Build WHERE conditions. We always filter tombstones.
	conditions := []string{"deleted_at IS NULL"}
	var args []any

	if q != nil && q.Filter != nil {
		clause, filterArgs := TranslateFilter(q.Filter, d)
		if clause != "" {
			// Re-number the filter placeholders to start after the tombstone
			// condition (which has no bind values). Since the tombstone check
			// has no parameters, the filter args start at position 1.
			conditions = append(conditions, clause)
			args = append(args, filterArgs...)
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Sort clause.
	sortFields := query.DefaultSort()
	if q != nil && len(q.Sort) > 0 {
		sortFields = q.Sort
	}
	orderClause := ""
	if orderBy := TranslateSort(sortFields, d); orderBy != "" {
		orderClause = "ORDER BY " + orderBy
	}

	// Pagination.
	limit := 50
	offset := 0
	if q != nil {
		if q.Limit > 0 {
			limit = q.Limit
		}
		if q.Offset > 0 {
			offset = q.Offset
		}
	}

	// Count query — separate from the data query to avoid double-pass issues
	// with some drivers that do not support LIMIT in subqueries inside COUNT.
	countQ := fmt.Sprintf(
		`SELECT COUNT(*) FROM (%s) AS _latest %s`,
		baseSubq, whereClause,
	)
	var total int64
	if err := r.conn.DB().QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, coreerrors.Internal("failed to count documents", err)
	}

	// Data query.
	// When using positional placeholders (postgres) we need to renumber the
	// limit/offset placeholders so they follow the filter args.
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2

	dataQ := fmt.Sprintf(
		`SELECT %s FROM (%s) AS _latest %s %s LIMIT %s OFFSET %s`,
		selectCols,
		baseSubq,
		whereClause,
		orderClause,
		d.Placeholder(limitIdx),
		d.Placeholder(offsetIdx),
	)

	dataArgs := append(args, limit, offset) //nolint:gocritic // intentional append to a new slice

	rows, err := r.conn.DB().QueryContext(ctx, dataQ, dataArgs...)
	if err != nil {
		return nil, coreerrors.Internal("failed to list documents", err)
	}
	defer rows.Close()

	items := make([]*model.Document, 0, limit)
	for rows.Next() {
		doc, err := scanRow(rows)
		if err != nil {
			return nil, coreerrors.Internal("failed to scan document row", err)
		}
		items = append(items, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, coreerrors.Internal("failed to iterate document rows", err)
	}

	hasMore := int64(offset+len(items)) < total

	return &model.ListResult{
		Items:   items,
		Total:   total,
		HasMore: hasMore,
	}, nil
}

// Update applies a partial patch to a document, appending a new version row.
func (r *SQLRepository) Update(ctx context.Context, schemaName, entityID string, patch map[string]any) (*model.Document, error) {
	current, err := r.FindByID(ctx, schemaName, entityID)
	if err != nil {
		return nil, err
	}

	merged := jsonutil.DeepMerge(current.Data, patch)
	now := time.Now().UTC()
	etag := generateETag(merged)

	dataStr, err := marshalData(merged)
	if err != nil {
		return nil, coreerrors.Internal("failed to marshal updated document data", err)
	}

	d := r.dialect
	tbl := d.QuoteIdentifier(tableName(schemaName))

	tx, err := r.conn.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, coreerrors.Internal("failed to begin update transaction", err)
	}
	defer func() { _ = tx.Rollback() }()

	version, err := nextVersion(ctx, tx, schemaName, entityID, d)
	if err != nil {
		return nil, coreerrors.Internal("failed to compute next version", err)
	}

	cols := []string{
		"entity_id", "schema_name", "record_version", "schema_version",
		"data", "created_at", "updated_at", "created_by", "updated_by", "etag",
	}
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = d.Placeholder(i + 1)
	}

	q := fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s)`,
		tbl,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	nowStr := now.Format(time.RFC3339Nano)
	createdAtStr := current.CreatedAt.Format(time.RFC3339Nano)

	_, err = tx.ExecContext(ctx, q,
		entityID,
		schemaName,
		version,
		current.SchemaVersion,
		dataStr,
		createdAtStr,
		nowStr,
		current.CreatedBy,
		"",
		etag,
	)
	if err != nil {
		return nil, coreerrors.Internal("failed to insert updated document version", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, coreerrors.Internal("failed to commit update transaction", err)
	}

	return &model.Document{
		ID:            entityID,
		EntityID:      entityID,
		SchemaName:    schemaName,
		RecordVersion: version,
		SchemaVersion: current.SchemaVersion,
		Data:          merged,
		CreatedAt:     current.CreatedAt,
		CreatedBy:     current.CreatedBy,
		UpdatedAt:     now,
		ETag:          etag,
	}, nil
}

// Delete soft-deletes a document by appending a tombstone version row with
// deleted_at set to the current time.
func (r *SQLRepository) Delete(ctx context.Context, schemaName, entityID string) error {
	current, err := r.FindByID(ctx, schemaName, entityID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	dataStr, err := marshalData(current.Data)
	if err != nil {
		return coreerrors.Internal("failed to marshal tombstone data", err)
	}

	d := r.dialect
	tbl := d.QuoteIdentifier(tableName(schemaName))

	tx, err := r.conn.DB().BeginTx(ctx, nil)
	if err != nil {
		return coreerrors.Internal("failed to begin delete transaction", err)
	}
	defer func() { _ = tx.Rollback() }()

	version, err := nextVersion(ctx, tx, schemaName, entityID, d)
	if err != nil {
		return coreerrors.Internal("failed to compute next version for tombstone", err)
	}

	cols := []string{
		"entity_id", "schema_name", "record_version", "schema_version",
		"data", "created_at", "updated_at", "deleted_at", "created_by", "updated_by", "etag",
	}
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = d.Placeholder(i + 1)
	}

	q := fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s)`,
		tbl,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	nowStr := now.Format(time.RFC3339Nano)
	createdAtStr := current.CreatedAt.Format(time.RFC3339Nano)
	updatedAtStr := current.UpdatedAt.Format(time.RFC3339Nano)

	_, err = tx.ExecContext(ctx, q,
		entityID,
		schemaName,
		version,
		current.SchemaVersion,
		dataStr,
		createdAtStr,
		updatedAtStr,
		nowStr, // deleted_at
		current.CreatedBy,
		"",
		current.ETag,
	)
	if err != nil {
		return coreerrors.Internal("failed to insert tombstone row", err)
	}

	if err := tx.Commit(); err != nil {
		return coreerrors.Internal("failed to commit delete transaction", err)
	}

	return nil
}

// BulkCreate persists multiple documents in order. On any per-item error the
// operation stops and returns the error.
func (r *SQLRepository) BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error) {
	results := make([]*model.Document, 0, len(items))
	for _, data := range items {
		doc, err := r.Create(ctx, schemaName, data)
		if err != nil {
			return results, fmt.Errorf("sql: bulk create: %w", err)
		}
		results = append(results, doc)
	}
	return results, nil
}

// BulkUpdate applies per-item patches to existing documents. Items whose
// entity IDs cannot be found are silently skipped.
func (r *SQLRepository) BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	results := make([]*model.Document, 0, len(items))
	for _, item := range items {
		doc, err := r.Update(ctx, schemaName, item.EntityID, item.Data)
		if err != nil {
			if coreerrors.IsNotFound(err) {
				continue
			}
			return results, fmt.Errorf("sql: bulk update: %w", err)
		}
		results = append(results, doc)
	}
	return results, nil
}

// BulkDelete soft-deletes the documents identified by ids. IDs that do not
// exist are silently skipped.
func (r *SQLRepository) BulkDelete(ctx context.Context, schemaName string, ids []string) error {
	for _, id := range ids {
		if err := r.Delete(ctx, schemaName, id); err != nil {
			if coreerrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("sql: bulk delete: %w", err)
		}
	}
	return nil
}

// EnsureIndexes creates the standard compound unique index on
// (entity_id, record_version) and a descending index on created_at.
// It is idempotent and safe to call on every startup.
func (r *SQLRepository) EnsureIndexes(ctx context.Context, schemaName string) error {
	m := NewMigrator(r.conn, r.dialect)
	return m.ensureStandardIndexes(ctx, schemaName)
}
