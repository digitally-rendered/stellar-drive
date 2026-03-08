package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/digitally-rendered/stellar-drive/pkg/core/event"
	"github.com/digitally-rendered/stellar-drive/pkg/core/model"
	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
	"github.com/digitally-rendered/stellar-drive/pkg/core/query"
)

// ---------------------------------------------------------------------------
// InstrumentedService
// ---------------------------------------------------------------------------

// InstrumentedService wraps a port.Service implementation and records
// counters, error counters, and duration histograms for every method call.
// It is safe for concurrent use if the wrapped service is.
type InstrumentedService struct {
	inner    port.Service
	provider *Provider
}

// NewInstrumentedService returns an InstrumentedService that forwards calls
// to inner and records metrics via provider.
func NewInstrumentedService(svc port.Service, provider *Provider) *InstrumentedService {
	return &InstrumentedService{
		inner:    svc,
		provider: provider,
	}
}

// record is a small helper that captures start time, delegates the operation,
// then emits the duration and (when err != nil) an error counter.
func (s *InstrumentedService) record(op string, fn func() error) error {
	start := time.Now()
	s.provider.IncrCounter(fmt.Sprintf("stellar_drive.operation.%s.total", op), 1)

	err := fn()

	s.provider.RecordDuration(
		fmt.Sprintf("stellar_drive.operation.%s.duration", op),
		time.Since(start),
	)
	if err != nil {
		s.provider.IncrCounter(fmt.Sprintf("stellar_drive.operation.%s.errors", op), 1)
	}
	return err
}

// Create implements port.Service.
func (s *InstrumentedService) Create(
	ctx context.Context,
	schemaName string,
	input map[string]any,
) (*model.Document, error) {
	var doc *model.Document
	err := s.record("create", func() error {
		var e error
		doc, e = s.inner.Create(ctx, schemaName, input)
		return e
	})
	return doc, err
}

// FindByID implements port.Service.
func (s *InstrumentedService) FindByID(
	ctx context.Context,
	schemaName string,
	entityID string,
) (*model.Document, error) {
	var doc *model.Document
	err := s.record("find_by_id", func() error {
		var e error
		doc, e = s.inner.FindByID(ctx, schemaName, entityID)
		return e
	})
	return doc, err
}

// List implements port.Service.
func (s *InstrumentedService) List(
	ctx context.Context,
	schemaName string,
	q *query.Query,
) (*model.ListResult, error) {
	var result *model.ListResult
	err := s.record("list", func() error {
		var e error
		result, e = s.inner.List(ctx, schemaName, q)
		return e
	})
	return result, err
}

// Update implements port.Service.
func (s *InstrumentedService) Update(
	ctx context.Context,
	schemaName string,
	entityID string,
	input map[string]any,
) (*model.Document, error) {
	var doc *model.Document
	err := s.record("update", func() error {
		var e error
		doc, e = s.inner.Update(ctx, schemaName, entityID, input)
		return e
	})
	return doc, err
}

// Delete implements port.Service.
func (s *InstrumentedService) Delete(
	ctx context.Context,
	schemaName string,
	entityID string,
) error {
	return s.record("delete", func() error {
		return s.inner.Delete(ctx, schemaName, entityID)
	})
}

// BulkCreate implements port.Service.
func (s *InstrumentedService) BulkCreate(
	ctx context.Context,
	schemaName string,
	inputs []map[string]any,
) ([]*model.Document, error) {
	s.provider.SetGauge("stellar_drive.operation.bulk_create.batch_size", int64(len(inputs)))

	var docs []*model.Document
	err := s.record("bulk_create", func() error {
		var e error
		docs, e = s.inner.BulkCreate(ctx, schemaName, inputs)
		return e
	})
	return docs, err
}

// BulkUpdate implements port.Service.
func (s *InstrumentedService) BulkUpdate(
	ctx context.Context,
	schemaName string,
	items []model.BulkUpdateItem,
) ([]*model.Document, error) {
	s.provider.SetGauge("stellar_drive.operation.bulk_update.batch_size", int64(len(items)))

	var docs []*model.Document
	err := s.record("bulk_update", func() error {
		var e error
		docs, e = s.inner.BulkUpdate(ctx, schemaName, items)
		return e
	})
	return docs, err
}

// BulkDelete implements port.Service.
func (s *InstrumentedService) BulkDelete(
	ctx context.Context,
	schemaName string,
	ids []string,
) error {
	s.provider.SetGauge("stellar_drive.operation.bulk_delete.batch_size", int64(len(ids)))

	return s.record("bulk_delete", func() error {
		return s.inner.BulkDelete(ctx, schemaName, ids)
	})
}

// ---------------------------------------------------------------------------
// InstrumentedEventBus
// ---------------------------------------------------------------------------

// InstrumentedEventBus wraps an *event.Bus and records a counter and duration
// histogram for every Publish call, keyed by event type. Subscribe delegates
// to the inner bus unchanged.
type InstrumentedEventBus struct {
	inner    *event.Bus
	provider *Provider
}

// NewInstrumentedEventBus returns an InstrumentedEventBus that forwards calls
// to bus and records metrics via provider.
func NewInstrumentedEventBus(bus *event.Bus, provider *Provider) *InstrumentedEventBus {
	return &InstrumentedEventBus{
		inner:    bus,
		provider: provider,
	}
}

// Subscribe registers handler with the inner bus. It does not alter the
// handler or record any metrics.
func (b *InstrumentedEventBus) Subscribe(
	eventType event.Type,
	schemaName string,
	handler event.Handler,
) {
	b.inner.Subscribe(eventType, schemaName, handler)
}

// Publish records a counter and a duration histogram for the event type, then
// delegates to the inner bus. The counter is incremented regardless of whether
// Publish returns an error; the duration covers the full inner dispatch.
func (b *InstrumentedEventBus) Publish(ctx context.Context, evt *event.Event) error {
	eventType := string(evt.Type)
	b.provider.IncrCounter(fmt.Sprintf("stellar_drive.event.%s.total", eventType), 1)

	start := time.Now()
	err := b.inner.Publish(ctx, evt)
	b.provider.RecordDuration(
		fmt.Sprintf("stellar_drive.event.%s.duration", eventType),
		time.Since(start),
	)
	return err
}
