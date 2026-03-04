// Package timeutil provides clock abstractions that make time-dependent code
// testable without real-time dependencies.
package timeutil

import "time"

// Clock is a minimal interface for obtaining the current time. Accepting Clock
// instead of calling time.Now() directly allows callers to inject a MockClock
// in tests.
type Clock interface {
	Now() time.Time
}

// RealClock implements Clock using the system wall clock. All times are
// returned in UTC.
type RealClock struct{}

// Now returns the current UTC time.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// MockClock implements Clock with a fixed time. Use it in unit tests to make
// time-sensitive assertions deterministic.
//
//	mc := timeutil.MockClock{FixedTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
//	svc := NewService(mc)
type MockClock struct {
	FixedTime time.Time
}

// Now returns the fixed time set on the MockClock.
func (m MockClock) Now() time.Time { return m.FixedTime }
