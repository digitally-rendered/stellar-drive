package webhook

import (
	"sync"
	"time"
)

// DeadLetterEntry records a single failed webhook delivery for later
// inspection or manual retry.
type DeadLetterEntry struct {
	ID         string    `json:"id"`
	WebhookID  string    `json:"webhook_id"`
	URL        string    `json:"url"`
	EventType  string    `json:"event_type"`
	SchemaName string    `json:"schema_name"`
	Payload    []byte    `json:"payload"`
	LastError  string    `json:"last_error"`
	Attempts   int       `json:"attempts"`
	FailedAt   time.Time `json:"failed_at"`
}

// DeadLetter stores failed webhook deliveries in memory. It is safe for
// concurrent use. Use NewDeadLetter to construct an instance.
type DeadLetter struct {
	mu      sync.Mutex
	entries []DeadLetterEntry
}

// NewDeadLetter returns an initialised, empty DeadLetter store.
func NewDeadLetter() *DeadLetter {
	return &DeadLetter{}
}

// Add appends entry to the dead-letter store.
func (d *DeadLetter) Add(entry DeadLetterEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = append(d.entries, entry)
}

// List returns a copy of all stored dead-letter entries. The caller may safely
// mutate the returned slice without affecting the store.
func (d *DeadLetter) List() []DeadLetterEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]DeadLetterEntry, len(d.entries))
	copy(out, d.entries)
	return out
}

// Remove deletes the entry whose ID matches id. If no entry matches, Remove is
// a no-op.
func (d *DeadLetter) Remove(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	filtered := d.entries[:0]
	for _, e := range d.entries {
		if e.ID != id {
			filtered = append(filtered, e)
		}
	}
	d.entries = filtered
}

// Clear removes all entries from the dead-letter store.
func (d *DeadLetter) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = nil
}
