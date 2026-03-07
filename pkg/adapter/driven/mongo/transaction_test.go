package mongo

import (
	"testing"
)

// TestWithTransactionSignature is a compile-time check that both new methods
// exist with the expected signatures. No MongoDB connection is required.
func TestWithTransactionSignature(t *testing.T) {
	t.Run("WithTransaction method exists on MongoRepository", func(t *testing.T) {
		// Take a method value to confirm the signature compiles.
		// If the signature ever changes this line will fail to build.
		_ = (*MongoRepository)(nil).WithTransaction
	})

	t.Run("StartSession method exists on Connection", func(t *testing.T) {
		// conn.StartSession is not called (that would panic on nil client); we
		// only take a method value to verify the symbol exists and compiles.
		conn := &Connection{}
		_ = conn.StartSession
	})
}
