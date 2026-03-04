package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/digitally-rendered/stellar-drive/pkg/core/port"
)

// Dispatcher implements port.WebhookDispatcher. It holds webhook registrations
// in memory, signs each delivery with HMAC-SHA256, and dispatches deliveries
// asynchronously in a goroutine with exponential backoff retries. Failed
// deliveries that exhaust all retries are recorded in the DeadLetter store.
type Dispatcher struct {
	mu            sync.RWMutex
	registrations map[string]*port.WebhookRegistration
	client        *http.Client
	maxRetries    int
	dead          *DeadLetter
}

// NewDispatcher returns a Dispatcher with the given maximum retry count.
// maxRetries=0 means a single attempt with no retries.
func NewDispatcher(maxRetries int) *Dispatcher {
	return &Dispatcher{
		registrations: make(map[string]*port.WebhookRegistration),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		maxRetries: maxRetries,
		dead:       NewDeadLetter(),
	}
}

// DeadLetters returns the dead-letter store so callers can inspect or replay
// failed deliveries.
func (d *Dispatcher) DeadLetters() *DeadLetter {
	return d.dead
}

// Register stores reg in memory, keyed by reg.ID. Calling Register with an
// existing ID replaces the previous registration.
func (d *Dispatcher) Register(_ context.Context, reg *port.WebhookRegistration) error {
	if reg == nil {
		return fmt.Errorf("webhook: Register called with nil registration")
	}
	if reg.ID == "" {
		return fmt.Errorf("webhook: registration ID must not be empty")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	// Store a shallow copy so the caller cannot mutate our internal state.
	clone := *reg
	d.registrations[reg.ID] = &clone
	return nil
}

// Unregister removes the registration identified by id. It is not an error to
// unregister an ID that does not exist.
func (d *Dispatcher) Unregister(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.registrations, id)
	return nil
}

// ListRegistrations returns all stored registrations.
func (d *Dispatcher) ListRegistrations(_ context.Context) ([]*port.WebhookRegistration, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*port.WebhookRegistration, 0, len(d.registrations))
	for _, reg := range d.registrations {
		clone := *reg
		out = append(out, &clone)
	}
	return out, nil
}

// Dispatch finds all active registrations that match eventType and schemaName,
// then asynchronously delivers payload to each matching endpoint. Each
// delivery runs in its own goroutine so slow or failing endpoints do not block
// one another or the caller.
//
// Delivery errors for individual endpoints do not cause Dispatch to return an
// error; they are logged and recorded in the dead-letter store after all
// retries are exhausted.
func (d *Dispatcher) Dispatch(ctx context.Context, eventType string, schemaName string, payload []byte) error {
	d.mu.RLock()
	matches := d.matchingRegistrations(eventType, schemaName)
	d.mu.RUnlock()

	for _, reg := range matches {
		reg := reg // capture for goroutine
		go func() {
			// Use a detached context so the goroutine is not cancelled when
			// the originating request context ends.
			deliveryCtx := context.WithoutCancel(ctx)
			if err := d.deliver(deliveryCtx, reg, eventType, schemaName, payload); err != nil {
				slog.Default().ErrorContext(deliveryCtx, "webhook delivery failed after retries",
					"webhook_id", reg.ID,
					"url", reg.URL,
					"event_type", eventType,
					"schema_name", schemaName,
					"error", err,
				)
				d.dead.Add(DeadLetterEntry{
					ID:         uuid.NewString(),
					WebhookID:  reg.ID,
					URL:        reg.URL,
					EventType:  eventType,
					SchemaName: schemaName,
					Payload:    payload,
					LastError:  err.Error(),
					Attempts:   d.maxRetries + 1,
					FailedAt:   time.Now().UTC(),
				})
			}
		}()
	}
	return nil
}

// matchingRegistrations returns registrations eligible for delivery. Must be
// called with d.mu at least read-locked.
func (d *Dispatcher) matchingRegistrations(eventType, schemaName string) []*port.WebhookRegistration {
	var out []*port.WebhookRegistration
	for _, reg := range d.registrations {
		if !reg.Active {
			continue
		}
		if !matchesEventType(reg.Events, eventType) {
			continue
		}
		if reg.SchemaName != "" && reg.SchemaName != schemaName {
			continue
		}
		clone := *reg
		out = append(out, &clone)
	}
	return out
}

// matchesEventType reports whether eventType is covered by the registration's
// event list. An empty list means all events match.
func matchesEventType(events []string, eventType string) bool {
	if len(events) == 0 {
		return true
	}
	for _, e := range events {
		if e == eventType {
			return true
		}
	}
	return false
}

// deliver signs payload with HMAC-SHA256 and POSTs it to reg.URL, retrying
// with exponential backoff on failure.
func (d *Dispatcher) deliver(ctx context.Context, reg *port.WebhookRegistration, eventType, schemaName string, payload []byte) error {
	deliveryID := uuid.NewString()
	ts := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	sig := signPayload(reg.Secret, payload)

	attempt := 0
	return RetryWithBackoff(ctx, d.maxRetries, func() error {
		attempt++
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reg.URL, bytes.NewReader(payload))
		if err != nil {
			// A construction error is permanent; no point retrying.
			return fmt.Errorf("webhook: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Stellar-Signature", "sha256="+sig)
		req.Header.Set("X-Stellar-Event", eventType)
		req.Header.Set("X-Stellar-Schema", schemaName)
		req.Header.Set("X-Stellar-Delivery", deliveryID)
		req.Header.Set("X-Stellar-Timestamp", ts)

		resp, err := d.client.Do(req)
		if err != nil {
			return fmt.Errorf("webhook: POST %s (attempt %d): %w", reg.URL, attempt, err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		return fmt.Errorf("webhook: POST %s returned HTTP %d (attempt %d)", reg.URL, resp.StatusCode, attempt)
	})
}

// signPayload computes the HMAC-SHA256 of payload using secret and returns the
// hex-encoded digest.
func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload) //nolint:errcheck // hash.Hash.Write never returns an error
	return hex.EncodeToString(mac.Sum(nil))
}
