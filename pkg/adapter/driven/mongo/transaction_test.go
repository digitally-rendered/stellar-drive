package mongo

import (
	"context"
	"testing"
)

// TestWithTransactionSignature is a compile-time check that both new methods
// exist with the expected signatures. No MongoDB connection is required.
func TestWithTransactionSignature(t *testing.T) {
	t.Run("WithTransaction method exists on MongoRepository", func(t *testing.T) {
		// Declare a variable of the method-value type to confirm the signature
		// compiles. If the signature ever changes this line will fail to build.
		var _ func(ctx context.Context, fn func(ctx context.Context) error) error = (*MongoRepository)(nil).WithTransaction
	})

	t.Run("StartSession method exists on Connection", func(t *testing.T) {
		// conn.StartSession is not called (that would panic on nil client); we
		// only take a method value to verify the symbol exists and compiles.
		conn := &Connection{}
		_ = conn.StartSession
	})
}
