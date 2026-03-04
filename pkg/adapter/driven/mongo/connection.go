// Package mongo provides a MongoDB-backed implementation of the port.Repository
// and schema.Store interfaces using the official Go MongoDB driver v2.
package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Connection manages the MongoDB client and database reference.
// It is safe to use from multiple goroutines after Connect returns.
type Connection struct {
	client   *mongo.Client
	database *mongo.Database
	dbName   string
	uri      string
}

// NewConnection creates a Connection configured with the given URI and database
// name. Call Connect to establish the actual network connection.
func NewConnection(uri, dbName string) (*Connection, error) {
	if uri == "" {
		return nil, fmt.Errorf("mongo: uri must not be empty")
	}
	if dbName == "" {
		return nil, fmt.Errorf("mongo: dbName must not be empty")
	}
	return &Connection{
		uri:    uri,
		dbName: dbName,
	}, nil
}

// Connect dials MongoDB and verifies connectivity with a Ping.
// It is safe to call multiple times; subsequent calls are no-ops if the client
// is already connected.
func (c *Connection) Connect(ctx context.Context) error {
	if c.client != nil {
		return nil
	}

	opts := options.Client().ApplyURI(c.uri)
	client, err := mongo.Connect(opts)
	if err != nil {
		return fmt.Errorf("mongo: connect: %w", err)
	}

	c.client = client
	c.database = client.Database(c.dbName)

	if err := c.Ping(ctx); err != nil {
		// Disconnect best-effort; ignore secondary error.
		_ = client.Disconnect(ctx)
		c.client = nil
		c.database = nil
		return fmt.Errorf("mongo: ping after connect: %w", err)
	}

	return nil
}

// Close gracefully disconnects the MongoDB client and releases all resources.
func (c *Connection) Close(ctx context.Context) error {
	if c.client == nil {
		return nil
	}
	if err := c.client.Disconnect(ctx); err != nil {
		return fmt.Errorf("mongo: disconnect: %w", err)
	}
	c.client = nil
	c.database = nil
	return nil
}

// Database returns the mongo.Database handle for the configured database name.
// Panics if Connect has not been called.
func (c *Connection) Database() *mongo.Database {
	if c.database == nil {
		panic("mongo: Connection.Database called before Connect")
	}
	return c.database
}

// Collection returns a handle for the named collection within the configured
// database. Panics if Connect has not been called.
func (c *Connection) Collection(name string) *mongo.Collection {
	return c.Database().Collection(name)
}

// Ping sends a lightweight ping command to verify that the server is reachable.
func (c *Connection) Ping(ctx context.Context) error {
	if c.client == nil {
		return fmt.Errorf("mongo: not connected")
	}
	if err := c.client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("mongo: ping: %w", err)
	}
	return nil
}
