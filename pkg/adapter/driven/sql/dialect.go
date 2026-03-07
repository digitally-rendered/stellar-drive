package sql

import (
	"fmt"
	"strings"
)

// Dialect abstracts the SQL syntax differences between database engines.
// Implementations are provided for PostgreSQL and SQLite; additional dialects
// (MySQL, CockroachDB, etc.) can be added by implementing this interface.
type Dialect interface {
	// Placeholder returns the positional parameter placeholder for the nth
	// bind value (1-indexed). PostgreSQL uses $1, $2, …; SQLite and MySQL
	// use ? for every position.
	Placeholder(n int) string

	// AutoIncrement returns the column definition fragment for a
	// surrogate integer primary key that auto-increments.
	// PostgreSQL: "BIGSERIAL PRIMARY KEY"
	// SQLite:     "INTEGER PRIMARY KEY AUTOINCREMENT"
	AutoIncrement() string

	// JSONColumn returns the column type used to store JSON payloads.
	// PostgreSQL: "JSONB"
	// SQLite:     "TEXT"
	JSONColumn() string

	// UpsertSuffix returns the ON CONFLICT / ON DUPLICATE KEY clause used
	// to turn an INSERT into an upsert when inserting the same
	// (entity_id, record_version) pair. The adapter uses INSERT … for new
	// rows only, so this is typically "ON CONFLICT DO NOTHING".
	UpsertSuffix() string

	// QuoteIdentifier wraps an identifier (table name, column name) in the
	// appropriate quoting characters for the dialect.
	// PostgreSQL / SQLite: double-quotes
	// MySQL:               back-ticks
	QuoteIdentifier(s string) string

	// JSONExtract returns a SQL expression that extracts a scalar text value
	// from the JSON stored in the given column for the given field path.
	// PostgreSQL: column->>'field'
	// SQLite:     json_extract(column, '$.field')
	JSONExtract(column, field string) string

	// DriverName returns the database/sql driver name this dialect is designed
	// for (e.g. "postgres", "sqlite3"). Used by NewDialect.
	DriverName() string
}

// ---------------------------------------------------------------------------
// PostgresDialect
// ---------------------------------------------------------------------------

// PostgresDialect implements Dialect for PostgreSQL-compatible databases
// (PostgreSQL, CockroachDB, AlloyDB, etc.).
type PostgresDialect struct{}

func (PostgresDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (PostgresDialect) AutoIncrement() string    { return "BIGSERIAL PRIMARY KEY" }
func (PostgresDialect) JSONColumn() string       { return "JSONB" }
func (PostgresDialect) UpsertSuffix() string     { return "ON CONFLICT DO NOTHING" }
func (PostgresDialect) DriverName() string       { return "postgres" }
func (PostgresDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (PostgresDialect) JSONExtract(column, field string) string {
	return fmt.Sprintf("%s->>'%s'", column, strings.ReplaceAll(field, "'", "''"))
}

// ---------------------------------------------------------------------------
// SQLiteDialect
// ---------------------------------------------------------------------------

// SQLiteDialect implements Dialect for SQLite 3.x databases. SQLite uses ?
// placeholders and stores JSON as TEXT (the json_extract family of functions
// is available in all modern SQLite builds compiled with -DSQLITE_ENABLE_JSON1,
// which is the default since SQLite 3.38.0).
type SQLiteDialect struct{}

func (SQLiteDialect) Placeholder(_ int) string { return "?" }
func (SQLiteDialect) AutoIncrement() string    { return "INTEGER PRIMARY KEY AUTOINCREMENT" }
func (SQLiteDialect) JSONColumn() string       { return "TEXT" }
func (SQLiteDialect) UpsertSuffix() string     { return "ON CONFLICT DO NOTHING" }
func (SQLiteDialect) DriverName() string       { return "sqlite3" }
func (SQLiteDialect) QuoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
func (SQLiteDialect) JSONExtract(column, field string) string {
	return fmt.Sprintf("json_extract(%s, '$.%s')", column, strings.ReplaceAll(field, "'", "''"))
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

// NewDialect returns the Dialect implementation appropriate for driverName.
// Recognised driver names:
//
//   - "postgres", "pgx", "pgx/v5", "cockroachdb"  → PostgresDialect
//   - "sqlite3", "sqlite"                          → SQLiteDialect
//
// Unknown driver names default to SQLiteDialect (? placeholders) with a
// TEXT JSON column, which is broadly compatible.
func NewDialect(driverName string) Dialect {
	switch strings.ToLower(driverName) {
	case "postgres", "pgx", "pgx/v5", "cockroachdb":
		return PostgresDialect{}
	case "sqlite3", "sqlite":
		return SQLiteDialect{}
	default:
		// Safest default: ? placeholders and TEXT for JSON.
		return SQLiteDialect{}
	}
}
