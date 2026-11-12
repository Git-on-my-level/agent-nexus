// Package decide owns durable human decisions and their approved actions.
// Execution remains with the registered Nexus bridge; external source truth
// remains with its authority.
package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrForbidden        = errors.New("decision permission denied")
	ErrInvalid          = errors.New("invalid decision request")
	ErrConflict         = errors.New("decision revision or state conflict")
	ErrNotFound         = errors.New("decision record not found")
	ErrStale            = errors.New("approved source revision has changed")
	ErrUnavailable      = errors.New("decision capability is not configured")
	ErrNothingDelivered = fmt.Errorf("%w: Nothing has been delivered yet, so there is nothing to read back", ErrInvalid)
)

// DecisionConflict preserves immutable request-key intent while identifying the
// existing record the client can inspect. Source revisions are opaque, unordered.
type DecisionConflict struct{ ExistingDecisionID string }

func (e *DecisionConflict) Error() string {
	return "This request key already identifies a different proposal; inspect the existing decision or use a new request key"
}
func (e *DecisionConflict) Unwrap() error { return ErrConflict }

// NativeExecutionError distinguishes a confirmed local rejection from commit
// uncertainty. Only trusted native executors may assert this boundary.
type NativeExecutionError struct {
	Cause        error
	WriteStarted bool
}

func (e *NativeExecutionError) Error() string { return e.Cause.Error() }
func (e *NativeExecutionError) Unwrap() error { return e.Cause }

type SupersededDecisionError struct{ SupersededBy string }

func (e *SupersededDecisionError) Error() string { return "Decision has been superseded" }
func (e *SupersededDecisionError) Unwrap() error { return ErrConflict }

var ErrContextWorkCursor = fmt.Errorf("%w: cursor is not valid for a work-scoped context", ErrInvalid)
var ErrContextCursor = fmt.Errorf("%w: cursor is malformed or from another scope", ErrInvalid)

type Status string

const (
	AwaitingAnswer Status = "awaiting_answer"
	Answered       Status = "answered"
	Declined       Status = "declined"
	Pending        Status = "pending_delivery"
	Sending        Status = "sending"
	Delivered      Status = "delivered"
	Acknowledged   Status = "acknowledged"
	Reported       Status = "source_reported"
	Verified       Status = "verified"
	Failed         Status = "failed"
	Unknown        Status = "unknown"
	Superseded     Status = "superseded"
)

type Principal struct {
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	Human       bool   `json:"human"`
}

// Origin is retained on durable decisions for provenance. New proposals do not
// accept a caller-supplied origin.
type Origin struct {
	Transport      string `json:"transport"`
	TenantID       string `json:"tenant_id"`
	ChannelID      string `json:"channel_id"`
	ThreadID       string `json:"thread_id,omitempty"`
	ExternalUserID string `json:"external_user_id"`
}

type ContextPage struct {
	Asks        []any    `json:"asks,omitempty"`
	Decisions   []any    `json:"decisions,omitempty"`
	Activity    []any    `json:"activity,omitempty"`
	Items       []any    `json:"items"`
	NextCursor  string   `json:"next_cursor,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
}
type ActionPayload struct {
	EvidenceRefs   []string `json:"evidence_refs,omitempty"`
	Note           string   `json:"note,omitempty"`
	Phase          string   `json:"phase,omitempty"`
	ResolutionRefs []string `json:"resolution_refs,omitempty"`
}

type DecisionInput struct {
	ActorID        string         `json:"actor_id,omitempty"`
	ReplayKey      string         `json:"replay_key,omitempty"`
	Payload        *ActionPayload `json:"payload,omitempty"`
	RequestKey     string         `json:"request_key"`
	WorkRef        string         `json:"work_ref"`
	Instruction    string         `json:"instruction"`
	Scope          string         `json:"scope"`
	TargetRevision string         `json:"target_revision"`
	Origin         *Origin        `json:"origin,omitempty"`
}
type Decision struct {
	ProposalReplayJSON     json.RawMessage `json:"-"` // Original opt-in proposal HTTP receipt; never canonical decision state.
	ProposalReplayStatus   int             `json:"-"`
	AnswerReplayJSON       json.RawMessage `json:"-"`                          // Original keyed HTTP receipt; never part of the canonical decision body.
	SourceAuthority        string          `json:"source_authority,omitempty"` // Trusted routing snapshot, not caller input.
	WorkMissing            bool            `json:"-"`                          // Current work projection; never persisted.
	TargetCurrent          *bool           `json:"-"`
	AlreadyAtTarget        *bool           `json:"-"`
	Replayed               bool            `json:"-"` // Response-only proposal reuse; never persisted.
	ProposedBy             string          `json:"proposed_by,omitempty"`
	OriginKind             string          `json:"origin_kind,omitempty"`
	TurnID                 string          `json:"turn_id,omitempty"`
	SupersededByProposedBy string          `json:"superseded_by_proposed_by,omitempty"`
	SupersededByOriginKind string          `json:"superseded_by_origin_kind,omitempty"`
	SupersededBy           string          `json:"superseded_by,omitempty"`
	SupersededReason       string          `json:"superseded_reason,omitempty"`
	Supersedes             string          `json:"supersedes,omitempty"`
	SupersedesProposedBy   string          `json:"supersedes_proposed_by,omitempty"`
	SupersedesOriginKind   string          `json:"supersedes_origin_kind,omitempty"`
	CanAnswer              bool            `json:"can_answer"`
	Payload                *ActionPayload  `json:"payload,omitempty"`
	ID                     string          `json:"id"`
	WorkspaceID            string          `json:"workspace_id"`
	ActorID                string          `json:"actor_id"`
	WorkRef                string          `json:"work_ref"`
	Instruction            string          `json:"instruction"`
	Scope                  string          `json:"scope"`
	TargetRevision         string          `json:"target_revision"`
	Status                 Status          `json:"status"`
	Revision               int             `json:"revision"`
	Answer                 string          `json:"answer,omitempty"`
	AnsweredBy             string          `json:"answered_by,omitempty"`
	ActionID               string          `json:"action_id,omitempty"`
	Origin                 *Origin         `json:"origin,omitempty"`
	CreatedAt              time.Time       `json:"created_at"`
}
type AnswerInput struct {
	ActorID    string `json:"actor_id,omitempty"`
	RequestKey string `json:"request_key,omitempty"`
	Revision   int    `json:"revision"`
	Approve    bool   `json:"approve"`
	Text       string `json:"text"`
}
type Action struct {
	DeliveryPath           string         `json:"delivery_path"`
	SourceAuthority        string         `json:"source_authority,omitempty"`
	ClosedWithoutDelivery  bool           `json:"closed_without_delivery,omitempty"`
	AcknowledgedBy         string         `json:"acknowledged_by,omitempty"`
	AcknowledgedAt         *time.Time     `json:"acknowledged_at,omitempty"`
	CreatedAt              *time.Time     `json:"created_at,omitempty"`
	Deliverable            bool           `json:"deliverable"`
	ReconciliationConflict bool           `json:"reconciliation_conflict"`
	Payload                *ActionPayload `json:"payload,omitempty"`
	ID                     string         `json:"id"`
	DecisionID             string         `json:"decision_id"`
	WorkspaceID            string         `json:"workspace_id"`
	ActorID                string         `json:"actor_id"`
	WorkRef                string         `json:"work_ref"`
	Instruction            string         `json:"instruction"`
	Scope                  string         `json:"scope"`
	TargetRevision         string         `json:"target_revision"`
	AuthorizationBasis     string         `json:"authorization_basis"`
	Status                 Status         `json:"status"`
	Receipt                Receipt        `json:"receipt"`
	Attempts               []Attempt      `json:"attempts"`
	Revision               int            `json:"revision"`
}
type Attempt struct {
	SentAt     *time.Time `json:"sent_at,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     Status     `json:"status"`
	Receipt    Receipt    `json:"receipt"`
}
type Receipt struct {
	// NativeReadBack is an in-process attestation from a trusted canonical executor, never accepted from source JSON.
	NativeReadBack        bool     `json:"-"`
	Status                Status   `json:"status"`
	ExternalID            string   `json:"external_id,omitempty"`
	URL                   string   `json:"url,omitempty"`
	EvidenceRefs          []string `json:"evidence_refs,omitempty"`
	IndependentlyVerified bool     `json:"independently_verified"`
	Detail                string   `json:"detail,omitempty"`
}

type Config struct {
	WorkspaceID string
}

// Dependencies are trusted server wiring, never supplied by a request or model.
// Authorize must consult current principal permissions on EVERY operation.
// Execute must atomically enforce TargetRevision at the source if supported;
// otherwise it must fail closed when a race cannot be excluded. The action ID
// is the stable remote idempotency key. DeliveryPath (or legacy CheckDelivery)
// is a required read-only preflight using the same routing as Execute. Missing
// executors must be rejected there, before recording any attempt. Errors after
// Execute starts are uncertain.
// Reconcile is read-only.
type Dependencies struct {
	// Read-only page-local delivery routing; never used by dispatch/mutations.
	BeginDeliveryRead func(context.Context, []string) (context.Context, error)
	// AuthorizeReadBatch checks a bounded read operation against fresh principal
	// authority and canonical resource visibility. It is never used for writes.
	// Missing entries deny access; nil retains the per-record authorizer.
	AuthorizeReadBatch func(context.Context, Principal, string, []string) (map[string]bool, error)
	// DecisionWork reads one live snapshot; ErrNotFound includes trashed/archived work.
	// ResolveResolution must apply the requesting principal's resource visibility
	// to any projected summary: decisions are workspace-readable while their
	// evidence may not be.
	// DecisionWorkBatch is read-only projection enrichment for a bounded page.
	// Missing map entries mean work is no longer live. Authorization remains
	// separate, using fresh read batches when configured or the normal per-record
	// permission checks.
	DecisionWorkBatch func(context.Context, Principal, []string) (map[string]DecisionWork, error)
	DecisionWork      func(context.Context, Principal, string) (DecisionWork, error)
	ResolveResolution func(context.Context, Principal, string) (ResolutionRef, error)
	Authorize         func(context.Context, Principal, string, string) error
	ReadContext       func(context.Context, Principal, string, string, int) (ContextPage, error)
	ReadContextPage   func(context.Context, Principal, string, string, string, int) (ContextPage, error)
	CurrentRevision   func(context.Context, Principal, string) (string, error)
	// DeliveryPath is the named preflight, using the same registry as Execute.
	// CheckDelivery supports older integrations without a named route.
	DeliveryPath  func(context.Context, Action) (string, error)
	CheckDelivery func(context.Context, Action) error
	Execute       func(context.Context, Action) (Receipt, error)
	Reconcile     func(context.Context, Action) (Receipt, error)
}

type DecisionWork struct {
	SourceAuthority string
	Revision        string
	Phase           string
}

// HumanProposalPendingError protects an awaiting human proposal under the store lock.
type HumanProposalPendingError struct {
	PendingDecisionID string `json:"pending_decision_id"`
}

func (e *HumanProposalPendingError) Error() string {
	return fmt.Sprintf("Human proposal %s is pending; a human must decline or answer it first.", e.PendingDecisionID)
}
func (e *HumanProposalPendingError) Unwrap() error { return ErrConflict }

// ConversationOwnerNotHumanError explains a proposal refusal after the runtime
// has resolved the conversation owner's actual principal kind.
type ConversationOwnerNotHumanError struct{}

func (e *ConversationOwnerNotHumanError) Error() string {
	return "Proposals need a human approver; this conversation belongs to an agent principal"
}
func (e *ConversationOwnerNotHumanError) Unwrap() error { return ErrForbidden }

// ApprovalTargetError describes proposal or approved-action source preflight failure.
type ApprovalTargetError struct {
	Proposal         bool `json:"-"`
	message          string
	OriginKind       string  `json:"origin_kind"`
	ProposedBy       string  `json:"proposed_by"`
	ApprovedRevision string  `json:"approved_revision"`
	CurrentRevision  *string `json:"current_revision"`
	Reason           string  `json:"reason"`
}

func (e *ApprovalTargetError) Error() string {
	if e.message != "" {
		return e.message
	}
	current := "unavailable"
	if e.CurrentRevision != nil {
		current = *e.CurrentRevision
	}
	if e.Proposal {
		return fmt.Sprintf("Proposal target has changed (proposed at %s, source now %s). Approval refused (%s); decline it or wait for a fresh proposal.", e.ApprovedRevision, current, e.Reason)
	}
	return fmt.Sprintf("Approved source revision has changed (approved at %s, source now %s). Approval refused (%s); inspect the work and create a fresh proposal if needed.", e.ApprovedRevision, current, e.Reason)
}
func (e *ApprovalTargetError) Unwrap() error { return ErrStale }
