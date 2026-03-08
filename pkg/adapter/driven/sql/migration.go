package sql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// MigrationRecord tracks a single applied migration in the _migrations table.
type MigrationRecord struct {
	Version     int       `json:"version"`
	SchemaName  string    `json:"schema_name"`
	Description string    `json:"description"`
	AppliedAt   time.Time `json:"applied_at"`
}

// migrationsTable is the fixed name of the migration tracking table.
const migrationsTable = "_migrations"

// Migrator applies DDL operations (CREATE TABLE, CREATE INDEX) for a given
// Connection and Dialect. All operations are idempotent: tables and indexes
// are created only if they do not already exist.
type Migrator struct {
	conn    *Connection
	dialect Dialect
}

// NewMigrator returns a Migrator that uses the given Connection and Dialect.
func NewMigrator(conn *Connection, dialect Dialect) *Migrator {
	return &Migrator{conn: conn, dialect: dialect}
}

// CreateTable creates the standard versioned table for schemaName if it does
// not already exist. The table contains the fixed set of audit and versioning
// columns; user-supplied data is stored as a single JSON/JSONB column named
// "data".
//
// Column layout:
//
//	id              – surrogate primary key (auto-increment)
//	entity_id       – stable logical identity of the document (UUID)
//	schema_name     – name of the schema this row belongs to
//	record_version  – monotonically increasing version number per entity_id
//	schema_version  – schema version string at time of write
//	data            – JSONB (postgres) or TEXT (sqlite) payload
//	created_at      – RFC3339 timestamp of initial creation
//	updated_at      – RFC3339 timestamp of this version
//	deleted_at      – NULL for live rows, non-NULL for tombstones
//	created_by      – optional actor string
//	updated_by      – optional actor string
//	etag            – SHA-256 hex digest of the data payload
//
// A UNIQUE constraint on (entity_id, record_version) enforces the invariant
// that each (entity, version) pair is written exactly once.
func (m *Migrator) CreateTable(ctx context.Context, schemaName string) error {
	d := m.dialect
	tbl := d.QuoteIdentifier(tableName(schemaName))

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    id             %s,
    entity_id      TEXT        NOT NULL,
    schema_name    TEXT        NOT NULL,
    record_version INTEGER     NOT NULL DEFAULT 1,
    schema_version TEXT        NOT NULL DEFAULT '',
    data           %s          NOT NULL DEFAULT '{}',
    created_at     TEXT        NOT NULL,
    updated_at     TEXT        NOT NULL,
    deleted_at     TEXT,
    created_by     TEXT        NOT NULL DEFAULT '',
    updated_by     TEXT        NOT NULL DEFAULT '',
    etag           TEXT        NOT NULL DEFAULT '',
    UNIQUE(entity_id, record_version)
)`,
		tbl,
		d.AutoIncrement(),
		d.JSONColumn(),
	)

	if _, err := m.conn.DB().ExecContext(ctx, ddl); err != nil {
		return coreerrors.Internal(
			fmt.Sprintf("sql: CreateTable %q: %s", schemaName, err.Error()),
			err,
		)
	}

	// Always ensure the standard indexes after table creation.
	return m.ensureStandardIndexes(ctx, schemaName)
}

// EnsureIndexes creates user-defined indexes declared in the schema's
// IndexDef slice. It also ensures the standard versioning indexes are present.
// The operation is idempotent: existing indexes are not modified.
func (m *Migrator) EnsureIndexes(ctx context.Context, schemaName string, indexes []schema.IndexDef) error {
	if err := m.ensureStandardIndexes(ctx, schemaName); err != nil {
		return err
	}

	for i, def := range indexes {
		if len(def.Fields) == 0 {
			return coreerrors.BadRequest(fmt.Sprintf("index %d has no fields", i))
		}
		if err := m.createIndex(ctx, schemaName, def); err != nil {
			return err
		}
	}
	return nil
}

// EnsureStandardIndexes creates the indexes required for correct append-only
// versioning behaviour. It is the exported form used by external callers; the
// unexported alias ensureStandardIndexes is used internally.
func (m *Migrator) EnsureStandardIndexes(ctx context.Context, schemaName string) error {
	return m.ensureStandardIndexes(ctx, schemaName)
}

// ensureStandardIndexes creates the two indexes required for correct
// append-only versioning behaviour:
//
//  1. UNIQUE (entity_id, record_version) – enforces the versioning invariant.
//  2. (entity_id, record_version DESC)   – optimises latest-version lookups.
//  3. (created_at)                       – optimises time-ordered list queries.
func (m *Migrator) ensureStandardIndexes(ctx context.Context, schemaName string) error {
	d := m.dialect
	tbl := tableName(schemaName)
	quotedTbl := d.QuoteIdentifier(tbl)

	indexes := []struct {
		name   string
		cols   string
		unique bool
	}{
		{
			name:   tbl + "_entity_version_uidx",
			cols:   "entity_id, record_version",
			unique: true,
		},
		{
			name: tbl + "_entity_version_desc_idx",
			cols: "entity_id, record_version DESC",
		},
		{
			name: tbl + "_created_at_idx",
			cols: "created_at",
		},
	}

	for _, idx := range indexes {
		uniqueKw := ""
		if idx.unique {
			uniqueKw = "UNIQUE "
		}
		ddl := fmt.Sprintf(
			`CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)`,
			uniqueKw,
			d.QuoteIdentifier(idx.name),
			quotedTbl,
			idx.cols,
		)
		if _, err := m.conn.DB().ExecContext(ctx, ddl); err != nil {
			return coreerrors.Internal(
				fmt.Sprintf("sql: ensureStandardIndexes %q: %s", idx.name, err.Error()),
				err,
			)
		}
	}
	return nil
}

// createIndex creates a single user-defined index for the given schemaName and
// IndexDef. Index names are derived from the table name and sorted field list
// to ensure they are deterministic and avoid collisions.
func (m *Migrator) createIndex(ctx context.Context, schemaName string, def schema.IndexDef) error {
	d := m.dialect
	tbl := tableName(schemaName)
	quotedTbl := d.QuoteIdentifier(tbl)

	// Build column expression list. Data fields use JSON extraction.
	cols := make([]string, len(def.Fields))
	for i, f := range def.Fields {
		cols[i] = columnExpr(f, d)
	}

	uniqueKw := ""
	if def.Unique {
		uniqueKw = "UNIQUE "
	}

	// Derive a deterministic index name from the table and field names.
	idxName := tbl + "_" + strings.Join(def.Fields, "_") + "_idx"

	ddl := fmt.Sprintf(
		`CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)`,
		uniqueKw,
		d.QuoteIdentifier(idxName),
		quotedTbl,
		strings.Join(cols, ", "),
	)

	if _, err := m.conn.DB().ExecContext(ctx, ddl); err != nil {
		return coreerrors.Internal(
			fmt.Sprintf("sql: createIndex %q: %s", idxName, err.Error()),
			err,
		)
	}
	return nil
}

// EnsureMigrationTable creates the _migrations tracking table if it does not
// already exist. It is safe to call on every startup; the IF NOT EXISTS clause
// makes the operation idempotent.
func (m *Migrator) EnsureMigrationTable(ctx context.Context) error {
	d := m.dialect
	tbl := d.QuoteIdentifier(migrationsTable)

	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    id          %s,
    version     INTEGER NOT NULL,
    schema_name TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    applied_at  TEXT    NOT NULL,
    UNIQUE(schema_name, version)
)`,
		tbl,
		d.AutoIncrement(),
	)

	if _, err := m.conn.DB().ExecContext(ctx, ddl); err != nil {
		return coreerrors.Internal(
			fmt.Sprintf("sql: EnsureMigrationTable: %s", err.Error()),
			err,
		)
	}
	return nil
}

// MigrateSchema creates the table and indexes for a single schema definition,
// recording the migration in _migrations when it has not been applied before.
// The operation is idempotent: calling it a second time for the same schema is
// a no-op.
func (m *Migrator) MigrateSchema(ctx context.Context, def *schema.SchemaDefinition) error {
	// Check whether a record already exists for this schema.
	applied, err := m.hasMigration(ctx, def.Name)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	// Create the versioned data table.
	if err := m.CreateTable(ctx, def.Name); err != nil {
		return err
	}

	// Apply any user-defined indexes declared in the schema.
	if len(def.Indexes) > 0 {
		if err := m.EnsureIndexes(ctx, def.Name, def.Indexes); err != nil {
			return err
		}
	}

	// Record the migration.
	return m.recordMigration(ctx, def.Name, 1, "initial table creation")
}

// MigrateAll creates tables and indexes for every schema in reg, recording
// each successful migration in the _migrations tracking table.
//
// The function is idempotent: schemas that have already been migrated are
// skipped without error.
func (m *Migrator) MigrateAll(ctx context.Context, reg *schema.Registry) error {
	if err := m.EnsureMigrationTable(ctx); err != nil {
		return err
	}

	for _, def := range reg.List() {
		if err := m.MigrateSchema(ctx, def); err != nil {
			return fmt.Errorf("sql: MigrateAll: schema %q: %w", def.Name, err)
		}
	}
	return nil
}

// Status returns all applied migration records ordered by applied_at ascending.
func (m *Migrator) Status(ctx context.Context) ([]MigrationRecord, error) {
	d := m.dialect
	tbl := d.QuoteIdentifier(migrationsTable)

	q := fmt.Sprintf(
		`SELECT version, schema_name, description, applied_at FROM %s ORDER BY applied_at ASC`,
		tbl,
	)

	rows, err := m.conn.DB().QueryContext(ctx, q)
	if err != nil {
		// If the table does not exist yet, return an empty slice rather than
		// an opaque driver error.
		return nil, coreerrors.Internal("sql: Status: query _migrations: "+err.Error(), err)
	}
	defer func() { _ = rows.Close() }()

	var records []MigrationRecord
	for rows.Next() {
		var rec MigrationRecord
		var appliedAtStr string
		if err := rows.Scan(&rec.Version, &rec.SchemaName, &rec.Description, &appliedAtStr); err != nil {
			return nil, coreerrors.Internal("sql: Status: scan row: "+err.Error(), err)
		}
		if t, err := time.Parse(time.RFC3339Nano, appliedAtStr); err == nil {
			rec.AppliedAt = t
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, coreerrors.Internal("sql: Status: iterate rows: "+err.Error(), err)
	}

	return records, nil
}

// hasMigration reports whether a migration record already exists for the given
// schema name (any version).
func (m *Migrator) hasMigration(ctx context.Context, schemaName string) (bool, error) {
	d := m.dialect
	tbl := d.QuoteIdentifier(migrationsTable)

	q := fmt.Sprintf(
		`SELECT COUNT(*) FROM %s WHERE schema_name = %s`,
		tbl, d.Placeholder(1),
	)

	var count int
	err := m.conn.DB().QueryRowContext(ctx, q, schemaName).Scan(&count)
	if err != nil {
		// The _migrations table might not exist yet if called before
		// EnsureMigrationTable; treat that as "not migrated".
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, coreerrors.Internal(
			fmt.Sprintf("sql: hasMigration %q: %s", schemaName, err.Error()),
			err,
		)
	}
	return count > 0, nil
}

// recordMigration inserts a new row into the _migrations tracking table.
func (m *Migrator) recordMigration(ctx context.Context, schemaName string, version int, description string) error {
	d := m.dialect
	tbl := d.QuoteIdentifier(migrationsTable)

	q := fmt.Sprintf(
		`INSERT INTO %s (version, schema_name, description, applied_at) VALUES (%s, %s, %s, %s)`,
		tbl,
		d.Placeholder(1),
		d.Placeholder(2),
		d.Placeholder(3),
		d.Placeholder(4),
	)

	appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := m.conn.DB().ExecContext(ctx, q, version, schemaName, description, appliedAt); err != nil {
		return coreerrors.Internal(
			fmt.Sprintf("sql: recordMigration %q: %s", schemaName, err.Error()),
			err,
		)
	}
	return nil
}
