package scopes

import (
	"context"
	"time"
)

// Coverage is the common response envelope. Every ID must be authorized in the
// current response snapshot. A subtotal must never imply all-scopes coverage.
type Coverage struct {
	CoveredScopeIDs     []ID
	UnavailableScopeIDs []ID
	MoreScopes          bool
	ScopeCursor         string
	AsOf                time.Time
}

// Page is shared structure; B defines its typed Item and aliases Page[Item].
// Cursor strings on wire must be opaque, authenticated and encrypted.
type Page[T any] struct {
	Items      []T
	NextCursor string
	Coverage   Coverage
}

// StreamChange is an already-authorized event envelope, not an author-input DTO.
// C owns payload formation, wire framing, authority checks and byte budgets.
type StreamChange struct {
	ScopeID    ID
	Family     string
	Audience   string
	ResourceID string
	Sequence   int64
	Payload    []byte
}

type TickRequest struct {
	Selection RequestSelection
	Streams   []Stream
	Cursor    string
	Limit     int
}
type TickResult = Page[StreamChange]

// StreamTicker is implemented by C's scopestream.Store. A no-op/empty tick must
// not reveal wrong-audience activity; cursor heads advance only when consumed.
type StreamTicker interface {
	Tick(context.Context, TickRequest) (TickResult, error)
}

// StepRequest is an internal worker contract, never a request-body type. D owns
// checkpoint loading and cursor persistence; callers cannot supply a new cursor.
type StepRequest struct {
	JobID         string
	ExpectedEpoch int64
	LeaseToken    int64
	MaxRecords    int
	MaxBytes      int64
	MaxDuration   time.Duration
}
type StepResult struct {
	Visited                    int
	Changed                    int
	Complete                   bool
	PermanentPrivateExceptions int
}

// MigrationStepper is implemented by D's scopemigrate.Store. Checkpoint, writes
// and exception counts commit atomically; lease/epoch mismatch advances nothing.
type MigrationStepper interface {
	Step(context.Context, StepRequest) (StepResult, error)
}
