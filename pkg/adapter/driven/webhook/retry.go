package webhook

import (
	"context"
	"math"
	"time"
)

// RetryWithBackoff retries fn with exponential backoff up to maxRetries times.
// Delays between attempts are 1s, 2s, 4s, 8s, 16s, ... The first call to fn
// is attempt 0; if it succeeds the function returns nil immediately without
// sleeping. If all attempts are exhausted, the last error is returned.
//
// The retry loop respects ctx cancellation: if ctx is done before a retry
// fires, the context error is returned immediately.
func RetryWithBackoff(ctx context.Context, maxRetries int, fn func() error) error {
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt == maxRetries {
			break
		}

		delay := backoffDuration(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return err
}

// backoffDuration returns the delay for the given attempt index (0-indexed).
// attempt=0 → 1s, attempt=1 → 2s, attempt=2 → 4s, and so on.
func backoffDuration(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}
