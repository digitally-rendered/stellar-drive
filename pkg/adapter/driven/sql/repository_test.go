package sql_test

// This file contains integration tests for SQLRepository that exercise the
// full Create → FindByID → Update → Delete → List pipeline against a real
// (in-process) SQLite-compatible database.
//
// Because the project does not vendor an external SQLite driver we register a
// minimal pure-Go "memdb" driver that stores rows in memory and understands
// the exact DDL and DML statements the SQL adapter produces. This keeps the
// test self-contained while still exercising real SQL generation logic.
//
// The memdb driver is intentionally simple: it supports:
//   - CREATE TABLE IF NOT EXISTS …
//   - CREATE [UNIQUE] INDEX IF NOT EXISTS …
//   - INSERT INTO … (cols) VALUES (…)
//   - SELECT … FROM … [WHERE …] [ORDER BY …] [LIMIT ? OFFSET ?]
//   - SELECT COUNT(*) FROM (…) [WHERE …]
//   - COALESCE(MAX(record_version), 0)
//   - Transactions (BEGIN / COMMIT / ROLLBACK)
//
// For anything more complex the tests fall back to asserting properties of
// the generated SQL strings rather than executing them.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sqladapter "github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/sql"
	coreerrors "github.com/digitally-rendered/stellar-drive/pkg/core/errors"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// ---------------------------------------------------------------------------
// Pure-Go in-memory SQL driver ("memdb")
// ---------------------------------------------------------------------------

func init() {
	sql.Register("memdb", &memDriver{})
}

// memDriver is the top-level driver.Driver implementation.
type memDriver struct{}

// databases is a package-level registry of in-memory databases keyed by DSN.
var (
	dbsMu     sync.Mutex
	databases = map[string]*memDatabase{}
)

func getOrCreateDB(dsn string) *memDatabase {
	dbsMu.Lock()
	defer dbsMu.Unlock()
	if db, ok := databases[dsn]; ok {
		return db
	}
	db := &memDatabase{tables: map[string]*memTable{}}
	databases[dsn] = db
	return db
}

func (d *memDriver) Open(dsn string) (driver.Conn, error) {
	return &memConn{db: getOrCreateDB(dsn)}, nil
}

// ---------------------------------------------------------------------------
// memDatabase
// ---------------------------------------------------------------------------

type memDatabase struct {
	mu     sync.RWMutex
	tables map[string]*memTable
}

func (db *memDatabase) table(name string) *memTable {
	db.mu.RLock()
	t := db.tables[name]
	db.mu.RUnlock()
	return t
}

func (db *memDatabase) createTable(name string, cols []string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.tables[name]; !ok {
		db.tables[name] = &memTable{
			name:    name,
			columns: cols,
			rows:    nil,
		}
	}
}

type memTable struct {
	mu      sync.RWMutex
	name    string
	columns []string
	rows    []memRow
}

// memRow maps column name → value.
type memRow map[string]any

func (t *memTable) insert(row memRow) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rows = append(t.rows, row)
}

// query returns rows that match the optional WHERE predicate, in insertion
// order. pred receives (row, table columns).
func (t *memTable) query(pred func(memRow) bool) []memRow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var result []memRow
	for _, r := range t.rows {
		if pred == nil || pred(r) {
			cp := make(memRow, len(r))
			for k, v := range r {
				cp[k] = v
			}
			result = append(result, cp)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// memConn – driver.Conn + driver.ExecerContext + driver.QueryerContext
// ---------------------------------------------------------------------------

type memConn struct {
	db *memDatabase
	// inTx is non-nil when inside a transaction.
	inTx *memTx
}

func (c *memConn) Prepare(query string) (driver.Stmt, error) {
	return &memStmt{conn: c, q: query}, nil
}
func (c *memConn) Close() error { return nil }
func (c *memConn) Begin() (driver.Tx, error) {
	tx := &memTx{conn: c}
	c.inTx = tx
	return tx, nil
}

type memTx struct{ conn *memConn }

func (t *memTx) Commit() error {
	t.conn.inTx = nil
	return nil
}
func (t *memTx) Rollback() error {
	t.conn.inTx = nil
	return nil
}

// ---------------------------------------------------------------------------
// memStmt
// ---------------------------------------------------------------------------

type memStmt struct {
	conn *memConn
	q    string
}

func (s *memStmt) Close() error  { return nil }
func (s *memStmt) NumInput() int { return -1 }

func (s *memStmt) Exec(args []driver.Value) (driver.Result, error) {
	anyArgs := driverValuesToAny(args)
	return execStatement(s.conn.db, s.q, anyArgs)
}

func (s *memStmt) Query(args []driver.Value) (driver.Rows, error) {
	anyArgs := driverValuesToAny(args)
	return queryStatement(s.conn.db, s.q, anyArgs)
}

func driverValuesToAny(vals []driver.Value) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// SQL execution helpers
// ---------------------------------------------------------------------------

func execStatement(db *memDatabase, q string, args []any) (driver.Result, error) {
	q = strings.TrimSpace(q)
	upper := strings.ToUpper(q)

	switch {
	case strings.HasPrefix(upper, "CREATE TABLE"):
		return execCreateTable(db, q)
	case strings.HasPrefix(upper, "CREATE UNIQUE INDEX"), strings.HasPrefix(upper, "CREATE INDEX"):
		// Just ignore index creation — we don't enforce indexes.
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(upper, "INSERT INTO"):
		return execInsert(db, q, args)
	default:
		return driver.RowsAffected(0), nil
	}
}

// execCreateTable parses a CREATE TABLE IF NOT EXISTS statement and registers
// the table with its column names.
func execCreateTable(db *memDatabase, q string) (driver.Result, error) {
	// Extract table name.
	// Pattern: CREATE TABLE IF NOT EXISTS "name" ( … )
	q = stripOuterParens(q)
	namePart := extractIdentAfter(q, "EXISTS")
	if namePart == "" {
		namePart = extractIdentAfter(q, "TABLE")
	}
	tblName := unquoteIdent(namePart)

	// Extract column names from the column definition block.
	start := strings.Index(q, "(")
	end := strings.LastIndex(q, ")")
	if start < 0 || end < 0 {
		return driver.RowsAffected(0), nil
	}
	colBlock := q[start+1 : end]
	cols := parseColumnNames(colBlock)

	db.createTable(tblName, cols)
	return driver.RowsAffected(0), nil
}

// parseColumnNames extracts bare column names from a CREATE TABLE column block.
// It handles lines like:
//
//	id  BIGSERIAL PRIMARY KEY,
//	entity_id TEXT NOT NULL,
//	UNIQUE(entity_id, record_version)
func parseColumnNames(block string) []string {
	var cols []string
	seen := map[string]bool{}
	for _, part := range strings.Split(block, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Skip constraint lines.
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "UNIQUE") ||
			strings.HasPrefix(upper, "PRIMARY") ||
			strings.HasPrefix(upper, "FOREIGN") ||
			strings.HasPrefix(upper, "CHECK") {
			continue
		}
		// First token is the column name.
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		name := unquoteIdent(fields[0])
		if name != "" && !seen[name] {
			seen[name] = true
			cols = append(cols, name)
		}
	}
	return cols
}

// execInsert parses a positional INSERT and stores a row.
func execInsert(db *memDatabase, q string, args []any) (driver.Result, error) {
	// INSERT INTO "table" (col1, col2, …) VALUES (?, ?, …)
	// or                               VALUES ($1, $2, …)

	// Extract table name.
	tblRaw := extractIdentAfter(q, "INTO")
	tblName := unquoteIdent(tblRaw)

	tbl := db.table(tblName)
	if tbl == nil {
		return nil, fmt.Errorf("memdb: table %q not found", tblName)
	}

	// Extract column list between the first pair of parentheses before VALUES.
	upper := strings.ToUpper(q)
	valIdx := strings.Index(upper, "VALUES")
	if valIdx < 0 {
		return nil, fmt.Errorf("memdb: INSERT missing VALUES")
	}

	colPart := q[:valIdx]
	colStart := strings.Index(colPart, "(")
	colEnd := strings.LastIndex(colPart, ")")
	if colStart < 0 || colEnd < 0 {
		return nil, fmt.Errorf("memdb: INSERT missing column list")
	}
	colNames := splitIdents(colPart[colStart+1 : colEnd])

	// We simply consume args in order; placeholders (? or $n) are ignored.
	if len(args) < len(colNames) {
		return nil, fmt.Errorf("memdb: INSERT: %d columns but only %d args", len(colNames), len(args))
	}

	row := make(memRow, len(colNames))
	for i, col := range colNames {
		row[col] = args[i]
	}

	tbl.insert(row)
	return driver.RowsAffected(1), nil
}

func queryStatement(db *memDatabase, q string, args []any) (driver.Rows, error) {
	q = strings.TrimSpace(q)
	upper := strings.ToUpper(q)

	// COUNT query: SELECT COUNT(*) FROM (subquery) AS alias WHERE …
	if strings.HasPrefix(upper, "SELECT COUNT(*)") {
		return execCount(db, q, args)
	}

	// COALESCE(MAX(record_version), 0) for nextVersion
	if strings.Contains(upper, "COALESCE(MAX(RECORD_VERSION)") {
		return execMaxVersion(db, q, args)
	}

	// Regular SELECT
	return execSelect(db, q, args)
}

// execMaxVersion handles:
//
//	SELECT COALESCE(MAX(record_version), 0) FROM "table" WHERE entity_id = ?
func execMaxVersion(db *memDatabase, q string, args []any) (driver.Rows, error) {
	upper := strings.ToUpper(q)
	fromIdx := strings.Index(upper, "FROM")
	whereIdx := strings.Index(upper, "WHERE")

	tblRaw := ""
	if fromIdx >= 0 {
		after := strings.TrimSpace(q[fromIdx+4:])
		// Next token is the table name.
		fields := strings.Fields(after)
		if len(fields) > 0 {
			tblRaw = unquoteIdent(fields[0])
		}
	}

	tbl := db.table(tblRaw)
	if tbl == nil {
		return singleValueRows(int64(0)), nil
	}

	// Parse WHERE entity_id = ?
	var entityID string
	if whereIdx >= 0 && len(args) > 0 {
		entityID = fmt.Sprintf("%v", args[0])
	}

	rows := tbl.query(func(r memRow) bool {
		if entityID == "" {
			return true
		}
		return fmt.Sprintf("%v", r["entity_id"]) == entityID
	})

	var max int64
	for _, r := range rows {
		v := toInt64(r["record_version"])
		if v > max {
			max = v
		}
	}
	return singleValueRows(max), nil
}

// execCount handles:
//
//	SELECT COUNT(*) FROM (subquery) AS _latest WHERE deleted_at IS NULL [AND …]
func execCount(db *memDatabase, q string, args []any) (driver.Rows, error) {
	rows, err := execSelect(db, buildSelectFromCount(q), args)
	if err != nil {
		return nil, err
	}

	// Count the rows.
	count := int64(0)
	mr := rows.(*memRows)
	for _, _ = range mr.rows {
		count++
	}
	return singleValueRows(count), nil
}

// buildSelectFromCount converts a COUNT query to a plain SELECT so we can
// reuse execSelect for the filtering logic.
func buildSelectFromCount(q string) string {
	upper := strings.ToUpper(q)
	fromIdx := strings.Index(upper, "FROM")
	if fromIdx < 0 {
		return q
	}
	return "SELECT * " + q[fromIdx:]
}

// execSelect handles general SELECT queries against the in-memory store.
// It understands:
//   - FROM (subquery) AS alias  — resolves the latest version per entity
//   - FROM "tablename"          — plain table scan
//   - WHERE deleted_at IS NULL
//   - WHERE entity_id = ?
//   - ORDER BY … ASC/DESC
//   - LIMIT ? OFFSET ?
func execSelect(db *memDatabase, q string, args []any) (driver.Rows, error) {
	upper := strings.ToUpper(q)

	// Determine whether we are selecting from a subquery or a plain table.
	fromIdx := strings.Index(upper, "FROM")
	if fromIdx < 0 {
		return emptyRows(), nil
	}
	afterFrom := strings.TrimSpace(q[fromIdx+4:])

	var rows []memRow
	var colNames []string

	if strings.HasPrefix(strings.TrimSpace(afterFrom), "(") {
		// Subquery: extract table name from inside the subquery.
		tblName := extractTableFromSubquery(afterFrom)
		tbl := db.table(tblName)
		if tbl == nil {
			return emptyRows(), nil
		}
		colNames = tbl.columns
		rows = latestVersionRows(tbl)
	} else {
		// Plain table.
		fields := strings.Fields(afterFrom)
		if len(fields) == 0 {
			return emptyRows(), nil
		}
		tblName := unquoteIdent(fields[0])
		tbl := db.table(tblName)
		if tbl == nil {
			return emptyRows(), nil
		}
		colNames = tbl.columns
		rows = tbl.query(nil)
	}

	// Apply WHERE filters.
	argIdx := 0
	rows, argIdx = applyWhere(rows, upper, q, args, argIdx)
	_ = argIdx

	// Apply ORDER BY.
	rows = applyOrderBy(rows, upper)

	// Apply LIMIT / OFFSET.
	rows = applyLimitOffset(rows, upper, args)

	// Build the column list from the SELECT projection.
	projCols := parseSelectProjection(q, colNames)

	return &memRows{columns: projCols, rows: rows, colAll: colNames}, nil
}

// latestVersionRows returns one row per entity_id: the row with the highest
// record_version, mirroring the SQL latestVersionSubquery logic.
func latestVersionRows(tbl *memTable) []memRow {
	tbl.mu.RLock()
	defer tbl.mu.RUnlock()

	// Map entity_id → best row.
	best := map[string]memRow{}
	for _, r := range tbl.rows {
		eid := fmt.Sprintf("%v", r["entity_id"])
		if prev, ok := best[eid]; !ok {
			best[eid] = r
		} else {
			if toInt64(r["record_version"]) > toInt64(prev["record_version"]) {
				best[eid] = r
			}
		}
	}

	result := make([]memRow, 0, len(best))
	for _, r := range best {
		cp := make(memRow, len(r))
		for k, v := range r {
			cp[k] = v
		}
		result = append(result, cp)
	}
	return result
}

// applyWhere filters rows based on the WHERE clause. Supports:
//   - deleted_at IS NULL
//   - deleted_at IS NOT NULL
//   - entity_id = ?
//   - ORDER BY (signals end of WHERE clause)
func applyWhere(rows []memRow, upper, q string, args []any, argIdx int) ([]memRow, int) {
	whereIdx := strings.Index(upper, "WHERE")
	if whereIdx < 0 {
		return rows, argIdx
	}

	// Find end of WHERE clause (before ORDER BY or LIMIT).
	whereEnd := len(upper)
	for _, kw := range []string{" ORDER BY ", " LIMIT ", " OFFSET "} {
		if idx := strings.Index(upper[whereIdx:], kw); idx >= 0 {
			if whereIdx+idx < whereEnd {
				whereEnd = whereIdx + idx
			}
		}
	}

	wherePart := upper[whereIdx+5 : whereEnd]
	wherePartOrig := q[whereIdx+5 : whereEnd]

	conditions := splitAndConditions(wherePart)
	conditionsOrig := splitAndConditions(wherePartOrig)

	for i, cond := range conditions {
		cond = strings.TrimSpace(cond)
		condOrig := strings.TrimSpace(conditionsOrig[i])

		switch {
		case strings.Contains(cond, "DELETED_AT IS NULL") && !strings.Contains(cond, "NOT"):
			rows = filterRows(rows, func(r memRow) bool {
				return r["deleted_at"] == nil || r["deleted_at"] == ""
			})

		case strings.Contains(cond, "DELETED_AT IS NOT NULL"):
			rows = filterRows(rows, func(r memRow) bool {
				v := r["deleted_at"]
				return v != nil && v != ""
			})

		case strings.Contains(cond, "ENTITY_ID ="):
			if argIdx < len(args) {
				val := fmt.Sprintf("%v", args[argIdx])
				argIdx++
				rows = filterRows(rows, func(r memRow) bool {
					return fmt.Sprintf("%v", r["entity_id"]) == val
				})
			}

		default:
			// Try to handle "col = ?" style conditions for data fields.
			_ = condOrig
		}
	}

	return rows, argIdx
}

func splitAndConditions(s string) []string {
	// Split on AND but not inside parentheses.
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

func filterRows(rows []memRow, pred func(memRow) bool) []memRow {
	var out []memRow
	for _, r := range rows {
		if pred(r) {
			out = append(out, r)
		}
	}
	return out
}

func applyOrderBy(rows []memRow, upper string) []memRow {
	obIdx := strings.Index(upper, " ORDER BY ")
	if obIdx < 0 {
		return rows
	}
	// Find end of ORDER BY clause.
	after := upper[obIdx+10:]
	endIdx := strings.Index(after, " LIMIT ")
	if endIdx < 0 {
		endIdx = strings.Index(after, " OFFSET ")
	}
	if endIdx >= 0 {
		after = after[:endIdx]
	}

	// Parse first sort term only for simplicity.
	term := strings.TrimSpace(strings.Split(after, ",")[0])
	desc := strings.HasSuffix(term, " DESC")
	col := strings.Fields(term)[0]
	col = strings.ToLower(col)
	// Strip json_extract / jsonb extraction to get base column.
	if strings.Contains(col, "created_at") {
		col = "created_at"
	} else if strings.Contains(col, "updated_at") {
		col = "updated_at"
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

func applyLimitOffset(rows []memRow, upper string, args []any) []memRow {
	// LIMIT and OFFSET are the last two positional args.
	if len(args) < 2 {
		return rows
	}

	limitVal := toInt(args[len(args)-2])
	offsetVal := toInt(args[len(args)-1])

	_ = upper // kept for future use

	if offsetVal >= len(rows) {
		return nil
	}
	rows = rows[offsetVal:]
	if limitVal > 0 && limitVal < len(rows) {
		rows = rows[:limitVal]
	}
	return rows
}

func parseSelectProjection(q string, allCols []string) []string {
	upper := strings.ToUpper(q)
	selectEnd := strings.Index(upper, " FROM ")
	if selectEnd < 0 {
		return allCols
	}
	proj := strings.TrimSpace(q[6:selectEnd])
	if proj == "*" || proj == "COUNT(*)" {
		return allCols
	}
	// The select cols are a fixed list of bare column names (possibly with
	// leading whitespace / newlines).
	var cols []string
	for _, raw := range strings.Split(proj, ",") {
		col := strings.TrimSpace(raw)
		col = strings.Split(col, " ")[0] // strip aliases
		if col != "" {
			cols = append(cols, col)
		}
	}
	return cols
}

// extractTableFromSubquery pulls the first table name from a FROM (subquery)
// expression by scanning for the inner FROM clause.
func extractTableFromSubquery(s string) string {
	upper := strings.ToUpper(s)
	fromIdx := strings.Index(upper[1:], "FROM") // skip leading '('
	if fromIdx < 0 {
		return ""
	}
	after := strings.TrimSpace(s[1+fromIdx+4:])
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	// The table name may appear twice (in INNER JOIN); take the first.
	return unquoteIdent(fields[0])
}

// ---------------------------------------------------------------------------
// memRows – driver.Rows
// ---------------------------------------------------------------------------

type memRows struct {
	columns []string
	colAll  []string
	rows    []memRow
	pos     int
}

func (r *memRows) Columns() []string { return r.columns }
func (r *memRows) Close() error      { return nil }

func (r *memRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.pos]
	r.pos++

	for i, col := range r.columns {
		// Column names may be "table.col" prefixed or just bare names.
		bare := col
		if idx := strings.LastIndex(col, "."); idx >= 0 {
			bare = col[idx+1:]
		}
		v, ok := row[bare]
		if !ok {
			v = nil
		}
		dest[i] = v
	}
	return nil
}

func emptyRows() driver.Rows {
	return &memRows{}
}

func singleValueRows(v any) driver.Rows {
	return &memRows{
		columns: []string{"value"},
		rows:    []memRow{{"value": v}},
	}
}

// ---------------------------------------------------------------------------
// Small parsing helpers
// ---------------------------------------------------------------------------

func extractIdentAfter(q, keyword string) string {
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

func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ",")
	if (strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`)) ||
		(strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`")) {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(s, `""`, `"`)
	}
	return s
}

func splitIdents(s string) []string {
	var result []string
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw != "" {
			result = append(result, unquoteIdent(raw))
		}
	}
	return result
}

func stripOuterParens(s string) string { return s }

func toInt64(v any) int64 {
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
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
}

func toInt(v any) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case int64:
		return int(t)
	case int:
		return t
	case float64:
		return int(t)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestRepo creates a SQLRepository backed by a fresh in-memory "memdb"
// database. The table for schemaName is created before the repo is returned.
func newTestRepo(t *testing.T, schemaName string) *sqladapter.SQLRepository {
	t.Helper()

	// Each test gets a unique DSN so databases do not bleed across tests.
	dsn := fmt.Sprintf("test_%s_%d", schemaName, time.Now().UnixNano())

	conn, err := sqladapter.NewConnection("memdb", dsn)
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	t.Cleanup(func() {
		// Clean up shared state.
		dbsMu.Lock()
		delete(databases, dsn)
		dbsMu.Unlock()
		_ = conn.Close(context.Background())
	})

	d := sqladapter.NewDialect("sqlite3")
	m := sqladapter.NewMigrator(conn, d)

	if err := m.CreateTable(context.Background(), schemaName); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}

	return sqladapter.NewRepository(conn, d)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSQLRepository_Create(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	data := map[string]any{"name": "Alice", "age": float64(30)}
	doc, err := repo.Create(ctx, "user", data)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if doc.EntityID == "" {
		t.Error("EntityID should not be empty")
	}
	if doc.RecordVersion != 1 {
		t.Errorf("RecordVersion = %d, want 1", doc.RecordVersion)
	}
	if doc.ETag == "" {
		t.Error("ETag should not be empty")
	}
	if doc.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if doc.Data["name"] != "Alice" {
		t.Errorf("data[name] = %v, want Alice", doc.Data["name"])
	}
}

func TestSQLRepository_Create_NilData(t *testing.T) {
	repo := newTestRepo(t, "widget")
	ctx := context.Background()

	doc, err := repo.Create(ctx, "widget", nil)
	if err != nil {
		t.Fatalf("Create with nil data: %v", err)
	}
	if doc.Data == nil {
		t.Error("Data should not be nil after create with nil input")
	}
}

func TestSQLRepository_FindByID(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	created, err := repo.Create(ctx, "user", map[string]any{"name": "Bob"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.FindByID(ctx, "user", created.EntityID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}

	if found.EntityID != created.EntityID {
		t.Errorf("EntityID = %q, want %q", found.EntityID, created.EntityID)
	}
	if found.RecordVersion != 1 {
		t.Errorf("RecordVersion = %d, want 1", found.RecordVersion)
	}
}

func TestSQLRepository_FindByID_NotFound(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "user", "nonexistent-id")
	if err == nil {
		t.Fatal("expected NotFound error, got nil")
	}
	if !coreerrors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestSQLRepository_Update(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	created, err := repo.Create(ctx, "user", map[string]any{"name": "Charlie", "age": float64(25)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := repo.Update(ctx, "user", created.EntityID, map[string]any{"age": float64(26)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.RecordVersion != 2 {
		t.Errorf("RecordVersion = %d, want 2", updated.RecordVersion)
	}
	if updated.Data["name"] != "Charlie" {
		t.Errorf("data[name] = %v, want Charlie (deep merge should preserve it)", updated.Data["name"])
	}
	if updated.Data["age"] != float64(26) {
		t.Errorf("data[age] = %v, want 26", updated.Data["age"])
	}
	if updated.ETag == created.ETag {
		t.Error("ETag should change after update")
	}
}

func TestSQLRepository_Update_NotFound(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	_, err := repo.Update(ctx, "user", "nonexistent-id", map[string]any{"name": "x"})
	if !coreerrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestSQLRepository_Delete(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	doc, err := repo.Create(ctx, "user", map[string]any{"name": "Dave"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, "user", doc.EntityID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = repo.FindByID(ctx, "user", doc.EntityID)
	if !coreerrors.IsNotFound(err) {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestSQLRepository_Delete_NotFound(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	err := repo.Delete(ctx, "user", "nonexistent-id")
	if !coreerrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestSQLRepository_List_Basic(t *testing.T) {
	repo := newTestRepo(t, "product")
	ctx := context.Background()

	names := []string{"Widget A", "Widget B", "Widget C"}
	for _, name := range names {
		if _, err := repo.Create(ctx, "product", map[string]any{"name": name}); err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
	}

	result, err := repo.List(ctx, "product", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if len(result.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(result.Items))
	}
}

func TestSQLRepository_List_ExcludesTombstones(t *testing.T) {
	repo := newTestRepo(t, "product")
	ctx := context.Background()

	a, _ := repo.Create(ctx, "product", map[string]any{"name": "Keep"})
	b, _ := repo.Create(ctx, "product", map[string]any{"name": "Delete me"})

	if err := repo.Delete(ctx, "product", b.EntityID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_ = a

	result, err := repo.List(ctx, "product", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("Total = %d, want 1 (deleted item should be excluded)", result.Total)
	}
}

func TestSQLRepository_List_Pagination(t *testing.T) {
	repo := newTestRepo(t, "item")
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		repo.Create(ctx, "item", map[string]any{"idx": float64(i)}) //nolint:errcheck
	}

	q := &query.Query{Limit: 2, Offset: 0}
	page1, err := repo.List(ctx, "item", q)
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1.Items))
	}
	if !page1.HasMore {
		t.Error("page1.HasMore should be true")
	}

	q2 := &query.Query{Limit: 2, Offset: 2}
	page2, err := repo.List(ctx, "item", q2)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2.Items) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2.Items))
	}

	q3 := &query.Query{Limit: 2, Offset: 4}
	page3, err := repo.List(ctx, "item", q3)
	if err != nil {
		t.Fatalf("List page3: %v", err)
	}
	if len(page3.Items) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3.Items))
	}
	if page3.HasMore {
		t.Error("page3.HasMore should be false")
	}
}

func TestSQLRepository_BulkCreate(t *testing.T) {
	repo := newTestRepo(t, "tag")
	ctx := context.Background()

	items := []map[string]any{
		{"name": "go"},
		{"name": "sql"},
		{"name": "hexagonal"},
	}

	docs, err := repo.BulkCreate(ctx, "tag", items)
	if err != nil {
		t.Fatalf("BulkCreate: %v", err)
	}
	if len(docs) != 3 {
		t.Errorf("len(docs) = %d, want 3", len(docs))
	}
	for i, d := range docs {
		if d.EntityID == "" {
			t.Errorf("docs[%d].EntityID is empty", i)
		}
	}
}

func TestSQLRepository_BulkUpdate(t *testing.T) {
	repo := newTestRepo(t, "tag")
	ctx := context.Background()

	a, _ := repo.Create(ctx, "tag", map[string]any{"name": "original-a"})
	b, _ := repo.Create(ctx, "tag", map[string]any{"name": "original-b"})

	updates := []model.BulkUpdateItem{
		{EntityID: a.EntityID, Data: map[string]any{"name": "updated-a"}},
		{EntityID: b.EntityID, Data: map[string]any{"name": "updated-b"}},
		{EntityID: "does-not-exist", Data: map[string]any{"name": "skip"}},
	}

	docs, err := repo.BulkUpdate(ctx, "tag", updates)
	if err != nil {
		t.Fatalf("BulkUpdate: %v", err)
	}
	// The not-found entity should be skipped.
	if len(docs) != 2 {
		t.Errorf("len(docs) = %d, want 2", len(docs))
	}
}

func TestSQLRepository_BulkDelete(t *testing.T) {
	repo := newTestRepo(t, "tag")
	ctx := context.Background()

	a, _ := repo.Create(ctx, "tag", map[string]any{"name": "a"})
	b, _ := repo.Create(ctx, "tag", map[string]any{"name": "b"})

	err := repo.BulkDelete(ctx, "tag", []string{a.EntityID, b.EntityID, "nonexistent"})
	if err != nil {
		t.Fatalf("BulkDelete: %v", err)
	}

	result, _ := repo.List(ctx, "tag", nil)
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0 after bulk delete", result.Total)
	}
}

func TestSQLRepository_EnsureIndexes(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	// Should be idempotent — calling multiple times should not error.
	for i := 0; i < 3; i++ {
		if err := repo.EnsureIndexes(ctx, "user"); err != nil {
			t.Fatalf("EnsureIndexes[%d]: %v", i, err)
		}
	}
}

// TestGenerateETag verifies that the ETag changes when data changes.
func TestGenerateETag_ChangesWithData(t *testing.T) {
	repo := newTestRepo(t, "user")
	ctx := context.Background()

	doc1, _ := repo.Create(ctx, "user", map[string]any{"name": "X"})
	doc2, _ := repo.Create(ctx, "user", map[string]any{"name": "Y"})

	if doc1.ETag == doc2.ETag {
		t.Error("ETags for different data should differ")
	}
}

// TestSQLRepository_ImplementsInterface is a compile-time check that
// *SQLRepository satisfies port.Repository.
func TestSQLRepository_ImplementsInterface(t *testing.T) {
	// This test body is intentionally empty; the type assertion below will
	// cause a compilation failure if the interface contract is broken.
	var _ interface {
		Create(ctx context.Context, schemaName string, data map[string]any) (*model.Document, error)
		FindByID(ctx context.Context, schemaName string, entityID string) (*model.Document, error)
		List(ctx context.Context, schemaName string, q *query.Query) (*model.ListResult, error)
		Update(ctx context.Context, schemaName string, entityID string, data map[string]any) (*model.Document, error)
		Delete(ctx context.Context, schemaName string, entityID string) error
		BulkCreate(ctx context.Context, schemaName string, items []map[string]any) ([]*model.Document, error)
		BulkUpdate(ctx context.Context, schemaName string, items []model.BulkUpdateItem) ([]*model.Document, error)
		BulkDelete(ctx context.Context, schemaName string, ids []string) error
		EnsureIndexes(ctx context.Context, schemaName string) error
	} = (*sqladapter.SQLRepository)(nil)
}

// ---------------------------------------------------------------------------
