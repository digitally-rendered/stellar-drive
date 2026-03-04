package sql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/digitally-rendered/stellar-drive/internal/stringutil"
)

// tableName returns the SQL table name for a given schemaName.
// The schema name is lower-cased and pluralised, matching the MongoDB adapter's
// collection naming convention.
func tableName(schemaName string) string {
	return stringutil.Pluralize(schemaName)
}

// latestVersionSubquery returns a SQL subquery that, for each entity_id in the
// given table, selects the row with the maximum record_version. The subquery
// aliases the result as "latest" and exposes all columns of the original table.
//
// The caller can join against this subquery or use it inside a CTE to obtain
// the current (most recent) version of every entity, then filter tombstones
// with a WHERE deleted_at IS NULL predicate.
//
//	SELECT … FROM (latestVersionSubquery(…)) AS latest WHERE deleted_at IS NULL
func latestVersionSubquery(schemaName string, d Dialect) string {
	tbl := d.QuoteIdentifier(tableName(schemaName))

	// Inner query: for each entity_id find the maximum record_version.
	// Outer query: join back to get the full row for that (entity_id, version) pair.
	return fmt.Sprintf(
		`SELECT t.* FROM %s t
INNER JOIN (
    SELECT entity_id, MAX(record_version) AS max_version
    FROM %s
    GROUP BY entity_id
) mv ON t.entity_id = mv.entity_id AND t.record_version = mv.max_version`,
		tbl, tbl,
	)
}

// nextVersion returns the next record_version to use for an entity_id within
// a transaction. It reads the current MAX(record_version) from the table and
// returns MAX + 1 (or 1 if no rows exist yet for that entity_id).
//
// The query is executed within the supplied transaction so that the read and
// subsequent insert are atomic.
func nextVersion(ctx context.Context, tx *sql.Tx, schemaName, entityID string, d Dialect) (int, error) {
	tbl := d.QuoteIdentifier(tableName(schemaName))
	q := fmt.Sprintf(
		`SELECT COALESCE(MAX(record_version), 0) FROM %s WHERE entity_id = %s`,
		tbl, d.Placeholder(1),
	)

	var current int
	if err := tx.QueryRowContext(ctx, q, entityID).Scan(&current); err != nil {
		return 0, fmt.Errorf("sql: nextVersion: %w", err)
	}
	return current + 1, nil
}
