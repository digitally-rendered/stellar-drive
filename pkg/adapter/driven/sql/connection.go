// Package sql provides a database/sql-backed implementation of the
// port.Repository interface. It uses an append-only versioning strategy
// identical to the MongoDB adapter: every mutation inserts a new row;
// no existing row is ever modified or deleted in place.
//
// The package relies only on the standard library's database/sql package.
// Callers are responsible for importing the appropriate driver (e.g.
// github.com/mattn/go-sqlite3 or github.com/lib/pq) and registering it
// via sql.Register before calling NewConnection.
package sql

import (
	"context"
	"database/sql"
	"fmt"
)

// Connection wraps a *sql.DB and provides lifecycle management helpers.
// It is safe to use from multiple goroutines after NewConnection returns.
type Connection struct {
	db         *sql.DB
	driverName string
	dsn        string
}

// NewConnection opens a database connection for the given driverName and dsn,
// then pings the server to verify connectivity. The caller must import the
// appropriate driver package as a side effect before calling NewConnection.
//
// Example (sqlite3):
//
//	import _ "github.com/mattn/go-sqlite3"
//	conn, err := sql.NewConnection("sqlite3", "file:test.db?mode=memory&cache=shared")
func NewConnection(driverName, dsn string) (*Connection, error) {
	if driverName == "" {
		return nil, fmt.Errorf("sql: driverName must not be empty")
	}
	if dsn == "" {
		return nil, fmt.Errorf("sql: dsn must not be empty")
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("sql: open %s: %w", driverName, err)
	}

	c := &Connection{
		db:         db,
		driverName: driverName,
		dsn:        dsn,
	}

	ctx := context.Background()
	if err := c.ping(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sql: ping after open: %w", err)
	}

	return c, nil
}

// DB returns the underlying *sql.DB handle.
func (c *Connection) DB() *sql.DB {
	return c.db
}

// Close releases all resources held by the connection pool.
func (c *Connection) Close(ctx context.Context) error {
	if c.db == nil {
		return nil
	}
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("sql: close: %w", err)
	}
	c.db = nil
	return nil
}

// ping sends a lightweight ping to verify the server is reachable.
func (c *Connection) ping(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("sql: not connected")
	}
	if err := c.db.PingContext(ctx); err != nil {
		return fmt.Errorf("sql: ping: %w", err)
	}
	return nil
}
