package service

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// mockAuditStore records all entries written via Record.
type mockAuditStore struct {
	mu      sync.Mutex
	entries []*model.AuditEntry
}

func (m *mockAuditStore) Record(_ context.Context, entry *model.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockAuditStore) GetHistory(_ context.Context, _ string, _ ...port.AuditQueryOption) ([]*model.AuditEntry, error) {
	return nil, nil
}

func (m *mockAuditStore) GetUserActivity(_ context.Context, _ string, _ ...port.AuditQueryOption) ([]*model.AuditEntry, error) {
	return nil, nil
}

func (m *mockAuditStore) getEntries() []*model.AuditEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*model.AuditEntry, len(m.entries))
	copy(cp, m.entries)
	return cp
}

func TestAuditTrail_Register(t *testing.T) {
	tests := []struct {
		name       string
		trackReads bool
		events     []event.Type
		wantOps    []string
	}{
		{
			name:       "write events are recorded",
			trackReads: false,
			events:     []event.Type{event.PostCreate, event.PostUpdate, event.PostDelete},
			wantOps:    []string{"create", "update", "delete"},
		},
		{
			name:       "read events not recorded when trackReads is false",
			trackReads: false,
			events:     []event.Type{event.PostGet, event.PostList},
			wantOps:    nil,
		},
		{
			name:       "read events recorded when trackReads is true",
			trackReads: true,
			events:     []event.Type{event.PostGet, event.PostList},
			wantOps:    []string{"get", "list"},
		},
		{
			name:       "bulk events are recorded",
			trackReads: false,
			events:     []event.Type{event.PostBulkCreate, event.PostBulkUpdate, event.PostBulkDelete},
			wantOps:    []string{"bulk_create", "bulk_update", "bulk_delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockAuditStore{}
			bus := event.NewBus()

			trail := NewAuditTrail(store, WithTrackReads(tt.trackReads))
			trail.Register(bus)

			ctx := context.Background()
			for _, evtType := range tt.events {
				err := bus.Publish(ctx, &event.Event{
					Type:       evtType,
					SchemaName: "pet",
					Metadata:   map[string]any{"user_id": "user-1"},
				})
				require.NoError(t, err)
			}

			entries := store.getEntries()
			if tt.wantOps == nil {
				assert.Empty(t, entries)
				return
			}

			require.Len(t, entries, len(tt.wantOps))
			for i, wantOp := range tt.wantOps {
				assert.Equal(t, wantOp, entries[i].Operation)
				assert.Equal(t, "pet", entries[i].SchemaName)
				assert.Equal(t, "user-1", entries[i].UserID)
			}
		})
	}
}

func TestAuditTrail_ExtractsEntityID(t *testing.T) {
	store := &mockAuditStore{}
	bus := event.NewBus()

	trail := NewAuditTrail(store)
	trail.Register(bus)

	doc := &model.Document{EntityID: "entity-123"}

	err := bus.Publish(context.Background(), &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Result:     doc,
		Metadata:   map[string]any{},
	})
	require.NoError(t, err)

	entries := store.getEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "entity-123", entries[0].EntityID)
}
