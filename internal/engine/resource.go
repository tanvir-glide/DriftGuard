// Package engine holds the core resource abstractions that rule
// evaluation is built on.
package engine

import "time"

// Severity levels attached to a Drift.
const (
	SeverityCritical = "CRITICAL"
	SeverityHigh     = "HIGH"
	SeverityMedium   = "MEDIUM"
)

// Resource is a piece of server state that has a desired baseline.
type Resource interface {
	// Check compares the current state against the desired state.
	Check() (Drift, error)

	// Revert restores the desired state.
	Revert() error

	// ID returns the resource identifier from the rulebook.
	ID() string

	// Type returns the resource type, e.g. "ssh_config".
	Type() string
}

// Drift describes a deviation from the desired state.
type Drift struct {
	Resource  string
	Current   interface{}
	Desired   interface{}
	Severity  string // CRITICAL, HIGH, MEDIUM
	Timestamp time.Time
}
