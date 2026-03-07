package sql_test

// Tests for Migrator.MigrateAll, MigrateSchema, and Status.
//
// The tests reuse the "memdb" driver registered in repository_test.go.
// Because the memdb WHERE parser only understands a small set of conditions,
// we register a second "memdb2" driver whose query handler is extended to
// support the `schema_name = ?` filter used by hasMigration. This keeps the
// existing repository tests unaffected while allowing precise migration tests.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	sqladapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/sql"
	"github.com/digitally-rendered/stellar-drive/pkg/core/schema"
)

// ---------------------------------------------------------------------------
// memdb2 — extended in-memory SQL driver for migration tests
//
// Shares the same storage implementation as the repository_test.go memdb but
// supports a broader WHERE clause vocabulary (schema_name = ?, version = ?).
// ---------------------------------------------------------------------------

func init() {
	sql.Register("memdb2", &memdb2Driver{})
}

// ---------------------------------------------------------------------------
// Shared database store for memdb2.
// ---------------------------------------------------------------------------

var (
	db2sMu    sync.Mutex
	databases2 = map[string]*memdb2Database{}
)

func getOrCreateDB2(dsn string) *memdb2Database {
	db2sMu.Lock()
	defer db2sMu.Unlock()
	if db, ok := databases2[dsn]; ok {
		return db
	}
	db := &memdb2Database{tables: map[string]*memdb2Table{}}
	databases2[dsn] = db
	return db
}

// ---------------------------------------------------------------------------
// memdb2Driver
// ---------------------------------------------------------------------------

type memdb2Driver struct{}

func (d *memdb2Driver) Open(dsn string) (driver.Conn, error) {
	return &memdb2Conn{db: getOrCreateDB2(dsn)}, nil
}

// ---------------------------------------------------------------------------
// memdb2Database
// ---------------------------------------------------------------------------

type memdb2Database struct {
	mu     sync.RWMutex
	tables map[string]*memdb2Table
}

func (db *memdb2Database) table(name string) *memdb2Table {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.tables[name]
}

func (db *memdb2Database) createTable(name string, cols []string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.tables[name]; !ok {
		db.tables[name] = &memdb2Table{name: name, columns: cols}
	}
}

type memdb2Table struct {
	mu      sync.RWMutex
	name    string
	columns []string
	rows    []memdb2Row
	nextID  int64
}

type memdb2Row map[string]any

func (t *memdb2Table) insert(row memdb2Row) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	if _, ok := row["id"]; !ok {
		row["id"] = t.nextID
	}
	t.rows = append(t.rows, row)
}

func (t *memdb2Table) allRows() []memdb2Row {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]memdb2Row, len(t.rows))
	for i, r := range t.rows {
		cp := make(memdb2Row, len(r))
		for k, v := range r {
			cp[k] = v
		}
		out[i] = cp
	}
	return out
}

// ---------------------------------------------------------------------------
// memdb2Conn
// ---------------------------------------------------------------------------

type memdb2Conn struct {
	db   *memdb2Database
	inTx *memdb2Tx
}

func (c *memdb2Conn) Prepare(query string) (driver.Stmt, error) {
	return &memdb2Stmt{conn: c, q: query}, nil
}
func (c *memdb2Conn) Close() error { return nil }
func (c *memdb2Conn) Begin() (driver.Tx, error) {
	tx := &memdb2Tx{conn: c}
	c.inTx = tx
	return tx, nil
}

type memdb2Tx struct{ conn *memdb2Conn }

func (t *memdb2Tx) Commit() error   { t.conn.inTx = nil; return nil }
func (t *memdb2Tx) Rollback() error { t.conn.inTx = nil; return nil }

// ---------------------------------------------------------------------------
// memdb2Stmt
// ---------------------------------------------------------------------------

type memdb2Stmt struct {
	conn *memdb2Conn
	q    string
}

func (s *memdb2Stmt) Close() error  { return nil }
func (s *memdb2Stmt) NumInput() int { return -1 }

func (s *memdb2Stmt) Exec(args []driver.Value) (driver.Result, error) {
	return db2ExecStatement(s.conn.db, s.q, dv2ToAny(args))
}

func (s *memdb2Stmt) Query(args []driver.Value) (driver.Rows, error) {
	return db2QueryStatement(s.conn.db, s.q, dv2ToAny(args))
}

func dv2ToAny(vals []driver.Value) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// SQL execution
// ---------------------------------------------------------------------------

func db2ExecStatement(db *memdb2Database, q string, args []any) (driver.Result, error) {
	q = strings.TrimSpace(q)
	upper := strings.ToUpper(q)
	switch {
	case strings.HasPrefix(upper, "CREATE TABLE"):
		return db2CreateTable(db, q)
	case strings.HasPrefix(upper, "CREATE UNIQUE INDEX"), strings.HasPrefix(upper, "CREATE INDEX"):
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(upper, "INSERT INTO"):
		return db2Insert(db, q, args)
	default:
		return driver.RowsAffected(0), nil
	}
}

func db2CreateTable(db *memdb2Database, q string) (driver.Result, error) {
	// Extract table name between "EXISTS" (or "TABLE") and "(".
	upper := strings.ToUpper(q)
	var namePart string
	for _, kw := range []string{"EXISTS ", "TABLE "} {
		if idx := strings.Index(upper, kw); idx >= 0 {
			after := strings.TrimSpace(q[idx+len(kw):])
			fields := strings.Fields(after)
			if len(fields) > 0 {
				namePart = fields[0]
				break
			}
		}
	}
	tblName := db2UnquoteIdent(namePart)

	start := strings.Index(q, "(")
	end := strings.LastIndex(q, ")")
	if start < 0 || end < 0 || start >= end {
		db.createTable(tblName, nil)
		return driver.RowsAffected(0), nil
	}
	colBlock := q[start+1 : end]
	cols := db2ParseColumnNames(colBlock)
	db.createTable(tblName, cols)
	return driver.RowsAffected(0), nil
}

func db2ParseColumnNames(block string) []string {
	var cols []string
	seen := map[string]bool{}
	for _, part := range strings.Split(block, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "UNIQUE") ||
			strings.HasPrefix(upper, "PRIMARY") ||
			strings.HasPrefix(upper, "FOREIGN") ||
			strings.HasPrefix(upper, "CHECK") {
			continue
		}
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		name := db2UnquoteIdent(fields[0])
		if name != "" && !seen[name] {
			seen[name] = true
			cols = append(cols, name)
		}
	}
	return cols
}

func db2Insert(db *memdb2Database, q string, args []any) (driver.Result, error) {
	upper := strings.ToUpper(q)

	// Table name.
	tblRaw := db2ExtractIdentAfter(q, "INTO")
	tblName := db2UnquoteIdent(tblRaw)

	tbl := db.table(tblName)
	if tbl == nil {
		return nil, fmt.Errorf("memdb2: table %q not found", tblName)
	}

	valIdx := strings.Index(upper, "VALUES")
	if valIdx < 0 {
		return nil, fmt.Errorf("memdb2: INSERT missing VALUES")
	}
	colPart := q[:valIdx]
	colStart := strings.Index(colPart, "(")
	colEnd := strings.LastIndex(colPart, ")")
	if colStart < 0 || colEnd < 0 {
		return nil, fmt.Errorf("memdb2: INSERT missing column list")
	}
	colNames := db2SplitIdents(colPart[colStart+1 : colEnd])

	if len(args) < len(colNames) {
		return nil, fmt.Errorf("memdb2: INSERT: %d columns but %d args", len(colNames), len(args))
	}

	row := make(memdb2Row, len(colNames))
	for i, col := range colNames {
		row[col] = args[i]
	}
	tbl.insert(row)
	return driver.RowsAffected(1), nil
}

// ---------------------------------------------------------------------------
// Query execution
// ---------------------------------------------------------------------------

func db2QueryStatement(db *memdb2Database, q string, args []any) (driver.Rows, error) {
	q = strings.TrimSpace(q)
	upper := strings.ToUpper(q)

	if strings.HasPrefix(upper, "SELECT COUNT(*)") {
		return db2ExecCount(db, q, args)
	}
	if strings.Contains(upper, "COALESCE(MAX(RECORD_VERSION)") {
		return db2ExecMaxVersion(db, q, args)
	}
	return db2ExecSelect(db, q, args)
}

func db2ExecMaxVersion(db *memdb2Database, q string, args []any) (driver.Rows, error) {
	upper := strings.ToUpper(q)
	fromIdx := strings.Index(upper, "FROM")
	var tblRaw string
	if fromIdx >= 0 {
		after := strings.TrimSpace(q[fromIdx+4:])
		fields := strings.Fields(after)
		if len(fields) > 0 {
			tblRaw = db2UnquoteIdent(fields[0])
		}
	}

	tbl := db.table(tblRaw)
	if tbl == nil {
		return db2SingleValue(int64(0)), nil
	}

	whereIdx := strings.Index(upper, "WHERE")
	var entityID string
	if whereIdx >= 0 && len(args) > 0 {
		entityID = fmt.Sprintf("%v", args[0])
	}

	rows := tbl.allRows()
	if entityID != "" {
		var filtered []memdb2Row
		for _, r := range rows {
			if fmt.Sprintf("%v", r["entity_id"]) == entityID {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	var max int64
	for _, r := range rows {
		v := db2ToInt64(r["record_version"])
		if v > max {
			max = v
		}
	}
	return db2SingleValue(max), nil
}

func db2ExecCount(db *memdb2Database, q string, args []any) (driver.Rows, error) {
	// Convert SELECT COUNT(*) … to SELECT * … and execute.
	upper := strings.ToUpper(q)
	fromIdx := strings.Index(upper, "FROM")
	if fromIdx < 0 {
		return db2SingleValue(int64(0)), nil
	}
	selectQ := "SELECT * " + q[fromIdx:]
	rows, err := db2ExecSelect(db, selectQ, args)
	if err != nil {
		return nil, err
	}
	count := int64(len(rows.(*memdb2Rows).rows))
	return db2SingleValue(count), nil
}

func db2ExecSelect(db *memdb2Database, q string, args []any) (driver.Rows, error) {
	upper := strings.ToUpper(q)

	fromIdx := strings.Index(upper, "FROM")
	if fromIdx < 0 {
		return db2EmptyRows(), nil
	}
	afterFrom := strings.TrimSpace(q[fromIdx+4:])

	var rows []memdb2Row
	var colNames []string

	if strings.HasPrefix(strings.TrimSpace(afterFrom), "(") {
		// Subquery: find the inner table.
		tblName := db2ExtractTableFromSubquery(afterFrom)
		tbl := db.table(tblName)
		if tbl == nil {
			return db2EmptyRows(), nil
		}
		colNames = tbl.columns
		rows = db2LatestVersionRows(tbl)
	} else {
		fields := strings.Fields(afterFrom)
		if len(fields) == 0 {
			return db2EmptyRows(), nil
		}
		tblName := db2UnquoteIdent(fields[0])
		tbl := db.table(tblName)
		if tbl == nil {
			return db2EmptyRows(), nil
		}
		colNames = tbl.columns
		rows = tbl.allRows()
	}

	// Apply WHERE.
	rows = db2ApplyWhere(rows, upper, q, args)

	// Apply ORDER BY.
	rows = db2ApplyOrderBy(rows, upper)

	// Apply LIMIT / OFFSET.
	rows = db2ApplyLimitOffset(rows, args)

	projCols := db2ParseSelectProjection(q, colNames)
	return &memdb2Rows{columns: projCols, colAll: colNames, rows: rows}, nil
}

// db2LatestVersionRows returns the row with the highest record_version per entity_id.
func db2LatestVersionRows(tbl *memdb2Table) []memdb2Row {
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()

	best := map[string]memdb2Row{}
	for _, r := range tbl.rows {
		eid := fmt.Sprintf("%v", r["entity_id"])
		prev, ok := best[eid]
		if !ok || db2ToInt64(r["record_version"]) > db2ToInt64(prev["record_version"]) {
			cp := make(memdb2Row, len(r))
			for k, v := range r {
				cp[k] = v
			}
			best[eid] = cp
		}
	}

	result := make([]memdb2Row, 0, len(best))
	for _, r := range best {
		result = append(result, r)
	}
	return result
}

// db2ApplyWhere filters rows based on a WHERE clause.
// Supports: deleted_at IS NULL, deleted_at IS NOT NULL, entity_id = ?,
// schema_name = ?, version = ?, and bare col = ? for any string column.
func db2ApplyWhere(rows []memdb2Row, upper, q string, args []any) []memdb2Row {
	whereIdx := strings.Index(upper, "WHERE")
	if whereIdx < 0 {
		return rows
	}

	whereEnd := len(upper)
	for _, kw := range []string{" ORDER BY ", " LIMIT ", " OFFSET "} {
		if idx := strings.Index(upper[whereIdx:], kw); idx >= 0 {
			if whereIdx+idx < whereEnd {
				whereEnd = whereIdx + idx
			}
		}
	}

	wherePart := upper[whereIdx+5 : whereEnd]
	conditions := db2SplitAnd(wherePart)

	argIdx := 0
	for _, cond := range conditions {
		cond = strings.TrimSpace(cond)

		switch {
		case strings.Contains(cond, "DELETED_AT IS NOT NULL"):
			rows = db2Filter(rows, func(r memdb2Row) bool {
				v := r["deleted_at"]
				return v != nil && v != ""
			})

		case strings.Contains(cond, "DELETED_AT IS NULL"):
			rows = db2Filter(rows, func(r memdb2Row) bool {
				return r["deleted_at"] == nil || r["deleted_at"] == ""
			})

		case strings.HasSuffix(strings.TrimSpace(cond), "= ?") ||
			strings.Contains(cond, "= $"):
			// Generic col = ? handler: extract the column name.
			col := db2ExtractColFromEq(cond)
			if col != "" && argIdx < len(args) {
				val := fmt.Sprintf("%v", args[argIdx])
				argIdx++
				colCapture := col
				rows = db2Filter(rows, func(r memdb2Row) bool {
					return fmt.Sprintf("%v", r[colCapture]) == val
				})
			}
		}
	}

	return rows
}

// db2ExtractColFromEq parses "COLUMN_NAME = ?" or "COLUMN_NAME = $N" and
// returns the lower-cased column name.
func db2ExtractColFromEq(cond string) string {
	cond = strings.TrimSpace(cond)
	eqIdx := strings.Index(cond, "=")
	if eqIdx < 0 {
		return ""
	}
	col := strings.TrimSpace(cond[:eqIdx])
	// Strip table prefix if present (e.g. "t.entity_id").
	if dotIdx := strings.LastIndex(col, "."); dotIdx >= 0 {
		col = col[dotIdx+1:]
	}
	return strings.ToLower(col)
}

func db2SplitAnd(s string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i+4 < len(s) && strings.ToUpper(s[i:i+4]) == " AND" {
			parts = append(parts, s[start:i])
			start = i + 4
			i += 3
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func db2Filter(rows []memdb2Row, pred func(memdb2Row) bool) []memdb2Row {
	var out []memdb2Row
	for _, r := range rows {
		if pred(r) {
			out = append(out, r)
		}
	}
	return out
}

func db2ApplyOrderBy(rows []memdb2Row, upper string) []memdb2Row {
	obIdx := strings.Index(upper, " ORDER BY ")
	if obIdx < 0 {
		return rows
	}
	after := upper[obIdx+10:]
	if idx := strings.Index(after, " LIMIT "); idx >= 0 {
		after = after[:idx]
	}
	if idx := strings.Index(after, " OFFSET "); idx >= 0 {
		after = after[:idx]
	}
	term := strings.TrimSpace(strings.Split(after, ",")[0])
	desc := strings.HasSuffix(term, " DESC")
	col := strings.ToLower(strings.Fields(term)[0])
	if strings.Contains(col, "applied_at") {
		col = "applied_at"
	} else if strings.Contains(col, "created_at") {
		col = "created_at"
	} else if strings.Contains(col, "record_version") {
		col = "record_version"
	}

	sort.SliceStable(rows, func(i, j int) bool {
		vi := fmt.Sprintf("%v", rows[i][col])
		vj := fmt.Sprintf("%v", rows[j][col])
		if desc {
			return vi > vj
		}
		return vi < vj
	})
	return rows
}

func db2ApplyLimitOffset(rows []memdb2Row, args []any) []memdb2Row {
	if len(args) < 2 {
		return rows
	}
	limitVal := db2ToInt(args[len(args)-2])
	offsetVal := db2ToInt(args[len(args)-1])
	if offsetVal >= len(rows) {
		return nil
	}
	rows = rows[offsetVal:]
	if limitVal > 0 && limitVal < len(rows) {
		rows = rows[:limitVal]
	}
	return rows
}

func db2ParseSelectProjection(q string, allCols []string) []string {
	upper := strings.ToUpper(q)
	end := strings.Index(upper, " FROM ")
	if end < 0 {
		return allCols
	}
	proj := strings.TrimSpace(q[6:end])
	if proj == "*" || proj == "COUNT(*)" {
		return allCols
	}
	var cols []string
	for _, raw := range strings.Split(proj, ",") {
		col := strings.TrimSpace(raw)
		col = strings.Split(col, " ")[0]
		if col != "" {
			cols = append(cols, col)
		}
	}
	return cols
}

func db2ExtractTableFromSubquery(s string) string {
	upper := strings.ToUpper(s)
	fromIdx := strings.Index(upper[1:], "FROM")
	if fromIdx < 0 {
		return ""
	}
	after := strings.TrimSpace(s[1+fromIdx+4:])
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	return db2UnquoteIdent(fields[0])
}

// ---------------------------------------------------------------------------
// memdb2Rows – driver.Rows
// ---------------------------------------------------------------------------

type memdb2Rows struct {
	columns []string
	colAll  []string
	rows    []memdb2Row
	pos     int
}

func (r *memdb2Rows) Columns() []string { return r.columns }
func (r *memdb2Rows) Close() error      { return nil }

func (r *memdb2Rows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.pos]
	r.pos++
	for i, col := range r.columns {
		bare := col
		if idx := strings.LastIndex(col, "."); idx >= 0 {
			bare = col[idx+1:]
		}
		dest[i] = row[bare]
	}
	return nil
}

func db2EmptyRows() driver.Rows {
	return &memdb2Rows{}
}

func db2SingleValue(v any) driver.Rows {
	return &memdb2Rows{
		columns: []string{"value"},
		rows:    []memdb2Row{{"value": v}},
	}
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func db2ExtractIdentAfter(q, keyword string) string {
	upper := strings.ToUpper(q)
	idx := strings.Index(upper, strings.ToUpper(keyword))
	if idx < 0 {
		return ""
	}
	after := strings.TrimSpace(q[idx+len(keyword):])
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func db2UnquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ",")
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
		(strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`")) {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, `""`, `"`)
	}
	return s
}

func db2SplitIdents(s string) []string {
	var result []string
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			result = append(result, db2UnquoteIdent(raw))
		}
	}
	return result
}

func db2ToInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case string:
		var n int64
		fmt.Sscanf(t, "%d", &n)
		return n
	}
	return 0
}

func db2ToInt(v any) int {
	return int(db2ToInt64(v))
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestMigrator creates a Migrator backed by a fresh in-memory memdb2 database.
// The caller receives both the migrator and the connection; the connection is
// automatically closed at test cleanup.
func newTestMigrator(t *testing.T) (*sqladapter.Migrator, *sqladapter.Connection) {
	t.Helper()
	dsn := fmt.Sprintf("migtest_%d", time.Now().UnixNano())

	conn, err := sqladapter.NewConnection("memdb2", dsn)
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	t.Cleanup(func() {
		db2sMu.Lock()
		delete(databases2, dsn)
		db2sMu.Unlock()
		_ = conn.Close(context.Background())
	})

	d := sqladapter.NewDialect("sqlite3")
	return sqladapter.NewMigrator(conn, d), conn
}

// makeRegistry builds a schema.Registry pre-loaded with the given schema names.
// Each schema is minimal: no properties or indexes.
func makeRegistry(t *testing.T, names ...string) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	for _, name := range names {
		env := &schema.SchemaEnvelope{
			Name:    name,
			Version: "1.0.0",
			Schema:  map[string]any{"type": "object"},
		}
		if _, err := reg.Register(env); err != nil {
			t.Fatalf("Register schema %q: %v", name, err)
		}
	}
	return reg
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestMigrateAll(t *testing.T) {
	ctx := context.Background()
	migrator, _ := newTestMigrator(t)

	reg := makeRegistry(t, "pet", "order")

	if err := migrator.MigrateAll(ctx, reg); err != nil {
		t.Fatalf("MigrateAll: %v", err)
	}

	records, err := migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Status: got %d records, want 2", len(records))
	}

	names := make(map[string]bool, len(records))
	for _, r := range records {
		names[r.SchemaName] = true
		if r.Version != 1 {
			t.Errorf("record %q: Version = %d, want 1", r.SchemaName, r.Version)
		}
		if r.Description == "" {
			t.Errorf("record %q: Description is empty", r.SchemaName)
		}
		if r.AppliedAt.IsZero() {
			t.Errorf("record %q: AppliedAt is zero", r.SchemaName)
		}
	}
	for _, name := range []string{"pet", "order"} {
		if !names[name] {
			t.Errorf("expected migration record for schema %q", name)
		}
	}
}

func TestMigrateAllIdempotent(t *testing.T) {
	ctx := context.Background()
	migrator, _ := newTestMigrator(t)

	reg := makeRegistry(t, "user", "category")

	// First call.
	if err := migrator.MigrateAll(ctx, reg); err != nil {
		t.Fatalf("MigrateAll (first): %v", err)
	}

	// Second call must not error or produce duplicate records.
	if err := migrator.MigrateAll(ctx, reg); err != nil {
		t.Fatalf("MigrateAll (second): %v", err)
	}

	records, err := migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Status after two MigrateAll calls: got %d records, want 2", len(records))
	}
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	migrator, _ := newTestMigrator(t)

	// Ensure the migration table exists before querying it.
	if err := migrator.EnsureMigrationTable(ctx); err != nil {
		t.Fatalf("EnsureMigrationTable: %v", err)
	}

	// No migrations yet.
	records, err := migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status (empty): %v", err)
	}
	if len(records) != 0 {
		t.Errorf("Status (empty): got %d records, want 0", len(records))
	}

	// Apply migrations then check again.
	reg := makeRegistry(t, "tag")
	if err := migrator.MigrateAll(ctx, reg); err != nil {
		t.Fatalf("MigrateAll: %v", err)
	}

	records, err = migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status (after migrate): %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Status: got %d records, want 1", len(records))
	}
	if records[0].SchemaName != "tag" {
		t.Errorf("record.SchemaName = %q, want %q", records[0].SchemaName, "tag")
	}
	if records[0].Version != 1 {
		t.Errorf("record.Version = %d, want 1", records[0].Version)
	}
}

func TestMigrateSchema(t *testing.T) {
	ctx := context.Background()
	migrator, _ := newTestMigrator(t)

	if err := migrator.EnsureMigrationTable(ctx); err != nil {
		t.Fatalf("EnsureMigrationTable: %v", err)
	}

	env := &schema.SchemaEnvelope{
		Name:    "product",
		Version: "1.0.0",
		Schema:  map[string]any{"type": "object"},
		Indexes: []schema.IndexDef{
			{Fields: []string{"name"}, Unique: false},
		},
	}
	reg := schema.NewRegistry()
	def, err := reg.Register(env)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := migrator.MigrateSchema(ctx, def); err != nil {
		t.Fatalf("MigrateSchema: %v", err)
	}

	records, err := migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Status: got %d records, want 1", len(records))
	}

	// Calling MigrateSchema again must be idempotent.
	if err := migrator.MigrateSchema(ctx, def); err != nil {
		t.Fatalf("MigrateSchema (second call): %v", err)
	}
	records, err = migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("Status after two MigrateSchema calls: got %d, want 1", len(records))
	}
}

func TestMigrateAll_EmptyRegistry(t *testing.T) {
	ctx := context.Background()
	migrator, _ := newTestMigrator(t)

	reg := schema.NewRegistry()

	if err := migrator.MigrateAll(ctx, reg); err != nil {
		t.Fatalf("MigrateAll with empty registry: %v", err)
	}

	records, err := migrator.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("Status: got %d records, want 0", len(records))
	}
}
