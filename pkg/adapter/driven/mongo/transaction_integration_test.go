//go:build integration

package mongo

import (
	"context"
	"testing"
)

// TestWithTransactionIntegration exercises commit and abort paths against a
// live MongoDB replica set. Standalone servers do not support multi-document
// transactions; run with a replica set, e.g.:
//
//	make test-int
//
// or:
//
//	go test -tags=integration ./pkg/adapter/driven/mongo/...
func TestWithTransactionIntegration(t *testing.T) {
	ctx := context.Background()

	conn, err := NewConnection("mongodb://localhost:27017/?replicaSet=rs0", "stellar_txn_test")
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	if err := conn.Connect(ctx); err != nil {
		t.Skipf("MongoDB not available, skipping integration test: %v", err)
	}
	defer conn.Close(ctx) //nolint:errcheck

	repo := NewRepository(conn)
	if err := repo.EnsureIndexes(ctx, "pet"); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	t.Run("commit on success", func(t *testing.T) {
		var createdID string
		err := repo.WithTransaction(ctx, func(tc context.Context) error {
			doc, err := repo.Create(tc, "pet", map[string]any{"name": "Fido", "status": "available"})
			if err != nil {
				return err
			}
			createdID = doc.EntityID
			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction (commit): %v", err)
		}
		if _, err := repo.FindByID(ctx, "pet", createdID); err != nil {
			t.Fatalf("committed document not found: %v", err)
		}
	})

	t.Run("abort on error", func(t *testing.T) {
		var attemptedID string
		txErr := repo.WithTransaction(ctx, func(tc context.Context) error {
			doc, err := repo.Create(tc, "pet", map[string]any{"name": "Ghost"})
			if err != nil {
				return err
			}
			attemptedID = doc.EntityID
			return context.DeadlineExceeded // force abort
		})
		if txErr == nil {
			t.Fatal("expected transaction error, got nil")
		}
		if attemptedID != "" {
			if _, err := repo.FindByID(ctx, "pet", attemptedID); err == nil {
				t.Fatal("aborted document should not be visible after rollback")
			}
		}
	})
}
