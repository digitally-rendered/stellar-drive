package telemetry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/digitally-rendered/stellar-drive/pkg/adapter/driven/telemetry"
	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// ---------------------------------------------------------------------------
// mockService — records which methods were called and optionally returns errors
// ---------------------------------------------------------------------------

type methodCall struct {
	name string
}

type mockService struct {
	mu      sync.Mutex
	calls   []methodCall
	errNext error // if non-nil, returned by the next call
}

func (m *mockService) recordCall(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, methodCall{name: name})
	err := m.errNext
	m.errNext = nil
	return err
}

func (m *mockService) calledMethods() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, len(m.calls))
	for i, c := range m.calls {
		names[i] = c.name
	}
	return names
}

func (m *mockService) Create(_ context.Context, _ string, _ map[string]any) (*model.Document, error) {
	err := m.recordCall("Create")
	if err != nil {
		return nil, err
	}
	return &model.Document{EntityID: "e1"}, nil
}

func (m *mockService) FindByID(_ context.Context, _ string, _ string) (*model.Document, error) {
	err := m.recordCall("FindByID")
	if err != nil {
		return nil, err
	}
	return &model.Document{EntityID: "e1"}, nil
}

func (m *mockService) List(_ context.Context, _ string, _ *query.Query) (*model.ListResult, error) {
	err := m.recordCall("List")
	if err != nil {
		return nil, err
	}
	return &model.ListResult{Total: 1}, nil
}

func (m *mockService) Update(_ context.Context, _ string, _ string, _ map[string]any) (*model.Document, error) {
	err := m.recordCall("Update")
	if err != nil {
		return nil, err
	}
	return &model.Document{EntityID: "e1"}, nil
}

func (m *mockService) Delete(_ context.Context, _ string, _ string) error {
	return m.recordCall("Delete")
}

func (m *mockService) BulkCreate(_ context.Context, _ string, inputs []map[string]any) ([]*model.Document, error) {
	err := m.recordCall("BulkCreate")
	if err != nil {
		return nil, err
	}
	docs := make([]*model.Document, len(inputs))
	for i := range inputs {
		docs[i] = &model.Document{}
	}
	return docs, nil
}

func (m *mockService) BulkUpdate(_ context.Context, _ string, items []model.BulkUpdateItem) ([]*model.Document, error) {
	err := m.recordCall("BulkUpdate")
	if err != nil {
		return nil, err
	}
	docs := make([]*model.Document, len(items))
	for i := range items {
		docs[i] = &model.Document{}
	}
	return docs, nil
}

func (m *mockService) BulkDelete(_ context.Context, _ string, _ []string) error {
	return m.recordCall("BulkDelete")
}

// ---------------------------------------------------------------------------
// TestInstrumentedService_Create_RecordsMetrics
// ---------------------------------------------------------------------------

func TestInstrumentedService_Create_RecordsMetrics(t *testing.T) {
	provider := telemetry.NewProvider()
	svc := &mockService{}
	instrumented := telemetry.NewInstrumentedService(svc, provider)

	doc, err := instrumented.Create(context.Background(), "pet", map[string]any{"name": "Fido"})
	require.NoError(t, err)
	assert.Equal(t, "e1", doc.EntityID)

	snap := provider.Snapshot()

	// Total counter must be incremented exactly once.
	assert.Equal(t, int64(1), snap.Counters["stellar_drive.operation.create.total"],
		"create.total counter should be 1 after one successful call")

	// No error counter should exist (or should be zero).
	assert.Equal(t, int64(0), snap.Counters["stellar_drive.operation.create.errors"],
		"create.errors counter should be 0 on success")

	// Duration histogram must have recorded one observation.
	h, ok := snap.Histograms["stellar_drive.operation.create.duration"]
	require.True(t, ok, "create.duration histogram should exist")
	assert.Equal(t, int64(1), h.Count, "histogram should contain exactly one observation")
	assert.Greater(t, int64(h.Sum), int64(0), "histogram sum should be positive")
}

// ---------------------------------------------------------------------------
// TestInstrumentedService_Create_Error_IncrementsErrorCounter
// ---------------------------------------------------------------------------

func TestInstrumentedService_Create_Error_IncrementsErrorCounter(t *testing.T) {
	provider := telemetry.NewProvider()
	svc := &mockService{errNext: errors.New("db unavailable")}
	instrumented := telemetry.NewInstrumentedService(svc, provider)

	_, err := instrumented.Create(context.Background(), "pet", map[string]any{"name": "Fido"})
	require.Error(t, err)

	snap := provider.Snapshot()

	assert.Equal(t, int64(1), snap.Counters["stellar_drive.operation.create.total"],
		"create.total counter should be 1 even on error")
	assert.Equal(t, int64(1), snap.Counters["stellar_drive.operation.create.errors"],
		"create.errors counter should be 1 after an error")

	h, ok := snap.Histograms["stellar_drive.operation.create.duration"]
	require.True(t, ok, "duration histogram should be recorded even on error")
	assert.Equal(t, int64(1), h.Count)
}

// ---------------------------------------------------------------------------
// TestInstrumentedService_AllMethods_Delegate
// ---------------------------------------------------------------------------

func TestInstrumentedService_AllMethods_Delegate(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func(svc *telemetry.InstrumentedService) error
		// method names expected to appear in the mock's call log
		wantCall string
		// metric suffixes to verify exist with count >= 1
		counterKey string
		histoKey   string
	}{
		{
			name:       "Create",
			fn:         func(s *telemetry.InstrumentedService) error { _, e := s.Create(ctx, "pet", nil); return e },
			wantCall:   "Create",
			counterKey: "stellar_drive.operation.create.total",
			histoKey:   "stellar_drive.operation.create.duration",
		},
		{
			name:       "FindByID",
			fn:         func(s *telemetry.InstrumentedService) error { _, e := s.FindByID(ctx, "pet", "1"); return e },
			wantCall:   "FindByID",
			counterKey: "stellar_drive.operation.find_by_id.total",
			histoKey:   "stellar_drive.operation.find_by_id.duration",
		},
		{
			name:       "List",
			fn:         func(s *telemetry.InstrumentedService) error { _, e := s.List(ctx, "pet", nil); return e },
			wantCall:   "List",
			counterKey: "stellar_drive.operation.list.total",
			histoKey:   "stellar_drive.operation.list.duration",
		},
		{
			name:       "Update",
			fn:         func(s *telemetry.InstrumentedService) error { _, e := s.Update(ctx, "pet", "1", nil); return e },
			wantCall:   "Update",
			counterKey: "stellar_drive.operation.update.total",
			histoKey:   "stellar_drive.operation.update.duration",
		},
		{
			name:       "Delete",
			fn:         func(s *telemetry.InstrumentedService) error { return s.Delete(ctx, "pet", "1") },
			wantCall:   "Delete",
			counterKey: "stellar_drive.operation.delete.total",
			histoKey:   "stellar_drive.operation.delete.duration",
		},
		{
			name: "BulkCreate",
			fn: func(s *telemetry.InstrumentedService) error {
				_, e := s.BulkCreate(ctx, "pet", []map[string]any{{"name": "a"}, {"name": "b"}})
				return e
			},
			wantCall:   "BulkCreate",
			counterKey: "stellar_drive.operation.bulk_create.total",
			histoKey:   "stellar_drive.operation.bulk_create.duration",
		},
		{
			name: "BulkUpdate",
			fn: func(s *telemetry.InstrumentedService) error {
				_, e := s.BulkUpdate(ctx, "pet", []model.BulkUpdateItem{{EntityID: "1"}, {EntityID: "2"}})
				return e
			},
			wantCall:   "BulkUpdate",
			counterKey: "stellar_drive.operation.bulk_update.total",
			histoKey:   "stellar_drive.operation.bulk_update.duration",
		},
		{
			name: "BulkDelete",
			fn: func(s *telemetry.InstrumentedService) error {
				return s.BulkDelete(ctx, "pet", []string{"1", "2", "3"})
			},
			wantCall:   "BulkDelete",
			counterKey: "stellar_drive.operation.bulk_delete.total",
			histoKey:   "stellar_drive.operation.bulk_delete.duration",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := telemetry.NewProvider()
			mock := &mockService{}
			instrumented := telemetry.NewInstrumentedService(mock, provider)

			err := tc.fn(instrumented)
			require.NoError(t, err)

			// Verify delegation: the mock must have been called.
			assert.Contains(t, mock.calledMethods(), tc.wantCall,
				"inner service method %q should have been called", tc.wantCall)

			// Verify total counter.
			snap := provider.Snapshot()
			assert.Equal(t, int64(1), snap.Counters[tc.counterKey],
				"counter %q should be 1", tc.counterKey)

			// Verify duration histogram.
			h, ok := snap.Histograms[tc.histoKey]
			require.True(t, ok, "histogram %q should exist", tc.histoKey)
			assert.Equal(t, int64(1), h.Count)
		})
	}
}

// ---------------------------------------------------------------------------
// TestInstrumentedService_BulkMethods_RecordBatchSize
// ---------------------------------------------------------------------------

func TestInstrumentedService_BulkMethods_RecordBatchSize(t *testing.T) {
	ctx := context.Background()

	t.Run("BulkCreate records batch size gauge", func(t *testing.T) {
		provider := telemetry.NewProvider()
		mock := &mockService{}
		svc := telemetry.NewInstrumentedService(mock, provider)

		inputs := []map[string]any{{"a": 1}, {"b": 2}, {"c": 3}}
		_, err := svc.BulkCreate(ctx, "pet", inputs)
		require.NoError(t, err)

		snap := provider.Snapshot()
		assert.Equal(t, int64(3), snap.Gauges["stellar_drive.operation.bulk_create.batch_size"])
	})

	t.Run("BulkUpdate records batch size gauge", func(t *testing.T) {
		provider := telemetry.NewProvider()
		mock := &mockService{}
		svc := telemetry.NewInstrumentedService(mock, provider)

		items := []model.BulkUpdateItem{{EntityID: "1"}, {EntityID: "2"}}
		_, err := svc.BulkUpdate(ctx, "pet", items)
		require.NoError(t, err)

		snap := provider.Snapshot()
		assert.Equal(t, int64(2), snap.Gauges["stellar_drive.operation.bulk_update.batch_size"])
	})

	t.Run("BulkDelete records batch size gauge", func(t *testing.T) {
		provider := telemetry.NewProvider()
		mock := &mockService{}
		svc := telemetry.NewInstrumentedService(mock, provider)

		err := svc.BulkDelete(ctx, "pet", []string{"a", "b", "c", "d"})
		require.NoError(t, err)

		snap := provider.Snapshot()
		assert.Equal(t, int64(4), snap.Gauges["stellar_drive.operation.bulk_delete.batch_size"])
	})
}

// ---------------------------------------------------------------------------
// TestInstrumentedEventBus_Publish_RecordsDuration
// ---------------------------------------------------------------------------

func TestInstrumentedEventBus_Publish_RecordsDuration(t *testing.T) {
	provider := telemetry.NewProvider()
	bus := event.NewBus()
	instrumented := telemetry.NewInstrumentedEventBus(bus, provider)

	ctx := context.Background()
	evt := &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Timestamp:  time.Now(),
	}

	err := instrumented.Publish(ctx, evt)
	require.NoError(t, err)

	snap := provider.Snapshot()

	// Counter keyed by event type.
	assert.Equal(t, int64(1), snap.Counters["stellar_drive.event.post_create.total"],
		"event total counter should be 1")

	// Duration histogram.
	h, ok := snap.Histograms["stellar_drive.event.post_create.duration"]
	require.True(t, ok, "event duration histogram should exist")
	assert.Equal(t, int64(1), h.Count)
	assert.GreaterOrEqual(t, int64(h.Sum), int64(0))
}

// ---------------------------------------------------------------------------
// TestInstrumentedEventBus_Subscribe_Delegates
// ---------------------------------------------------------------------------

func TestInstrumentedEventBus_Subscribe_Delegates(t *testing.T) {
	provider := telemetry.NewProvider()
	bus := event.NewBus()
	instrumented := telemetry.NewInstrumentedEventBus(bus, provider)

	var called bool
	instrumented.Subscribe(event.PostCreate, "pet", func(_ context.Context, _ *event.Event) error {
		called = true
		return nil
	})

	err := instrumented.Publish(context.Background(), &event.Event{
		Type:       event.PostCreate,
		SchemaName: "pet",
		Timestamp:  time.Now(),
	})
	require.NoError(t, err)
	assert.True(t, called, "handler registered via Subscribe should have been invoked")
}

// ---------------------------------------------------------------------------
// TestInstrumentedEventBus_MultiplePublish_AccumulatesCounters
// ---------------------------------------------------------------------------

func TestInstrumentedEventBus_MultiplePublish_AccumulatesCounters(t *testing.T) {
	provider := telemetry.NewProvider()
	bus := event.NewBus()
	instrumented := telemetry.NewInstrumentedEventBus(bus, provider)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		err := instrumented.Publish(ctx, &event.Event{
			Type:       event.PreCreate,
			SchemaName: "order",
			Timestamp:  time.Now(),
		})
		require.NoError(t, err)
	}

	snap := provider.Snapshot()
	assert.Equal(t, int64(5), snap.Counters["stellar_drive.event.pre_create.total"])

	h := snap.Histograms["stellar_drive.event.pre_create.duration"]
	assert.Equal(t, int64(5), h.Count, "histogram should record all 5 publish calls")
}
