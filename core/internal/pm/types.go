// Package pm owns durable conversational PM handoffs. Execution remains with the
// registered Nexus bridge; external source truth remains with its authority.
package pm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"agent-nexus-core/internal/router"
)

var (
	ErrForbidden        = errors.New("PM permission denied")
	ErrInvalid          = errors.New("invalid PM request")
	ErrConflict         = errors.New("PM revision or state conflict")
	ErrNotFound         = errors.New("PM record not found")
	ErrTurnClosed       = errors.New("PM turn is closed")
	ErrStale            = errors.New("approved source revision has changed")
	ErrUnavailable      = errors.New("PM capability is not configured")
	ErrPMIdentity       = fmt.Errorf("%w: ANX_PM_AGENT_ACTOR_ID is required", ErrUnavailable)
	ErrNothingDelivered = fmt.Errorf("%w: Nothing has been delivered yet, so there is nothing to read back", ErrInvalid)
	ErrBusy             = errors.New("PM execution capacity reached")
	ErrEmpty            = errors.New("no claimable PM turn")
)

// TurnClosedError carries the durable turn state, independently of source revisions.
type TurnClosedError struct {
	TurnID   string    `json:"turn_id"`
	Deadline time.Time `json:"deadline"`
	Status   Status    `json:"status"`
	expired  bool
}

func (e *TurnClosedError) Error() string {
	if e.expired {
		return "This turn passed its deadline and was failed; nothing can be proposed or read for it. Ask again to start a new turn."
	}
	return "This turn is already terminal; nothing can be proposed or read for it. Ask again to start a new turn."
}
func (e *TurnClosedError) Unwrap() error { return ErrTurnClosed }

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

func (e *SupersededDecisionError) Error() string { return "PM decision has been superseded" }
func (e *SupersededDecisionError) Unwrap() error { return ErrConflict }

var ErrContextWorkCursor = fmt.Errorf("%w: cursor is not valid for a work-scoped context", ErrInvalid)
var ErrContextCursor = fmt.Errorf("%w: cursor is malformed or from another scope", ErrInvalid)

type Status string

const (
	AwaitingAnswer Status = "awaiting_answer"
	Answered       Status = "answered"
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
type Origin struct {
	Transport      string `json:"transport"`
	TenantID       string `json:"tenant_id"`
	ChannelID      string `json:"channel_id"`
	ThreadID       string `json:"thread_id,omitempty"`
	ExternalUserID string `json:"external_user_id"`
}
type Binding struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	ActorID     string `json:"actor_id"`
	Origin      Origin `json:"origin"`
	CanApprove  bool   `json:"can_approve"`
	Enabled     bool   `json:"enabled"`
	Revision    int    `json:"revision"`
}
type Conversation struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ActorID     string    `json:"actor_id"`
	Title       string    `json:"title"`
	WorkRef     string    `json:"work_ref,omitempty"`
	ThreadID    string    `json:"thread_id"`
	Origin      *Origin   `json:"origin,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
type CreateConversation struct {
	RequestKey string `json:"request_key"`
	Title      string `json:"title"`
	WorkRef    string `json:"work_ref,omitempty"`
}
type MessageInput struct {
	RequestKey string `json:"request_key"`
	Text       string `json:"text"`
}

// Turn is the durable record; HTTP responses use turnResponse to hide lease credentials.
type Turn struct {
	DecisionIDs    []string   `json:"decision_ids,omitempty"`
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	WorkspaceID    string     `json:"workspace_id"`
	ActorID        string     `json:"actor_id"`
	Text           string     `json:"text"`
	Response       string     `json:"response,omitempty"`
	Status         Status     `json:"status"`
	WakeupID       string     `json:"wakeup_id"`
	AgentActorID   string     `json:"agent_actor_id"`
	EvidenceRefs   []string   `json:"evidence_refs,omitempty"`
	Failure        string     `json:"failure,omitempty"`
	ClaimedAt      *time.Time `json:"claimed_at,omitempty"`
	LeaseToken     string     `json:"lease_token,omitempty"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt time.Time  `json:"lease_expires_at,omitempty"`
	MaxOutputBytes int        `json:"max_output_bytes,omitempty"`
	Origin         *Origin    `json:"origin,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Deadline       time.Time  `json:"deadline"`
	Revision       int        `json:"revision"`
}
type ClaimInput struct {
	RunnerID string `json:"runner_id"`
}
type FailInput struct {
	Reason     string `json:"reason"`
	LeaseToken string `json:"lease_token"`
}
type ConversationDetail struct {
	Conversation Conversation `json:"conversation"`
	Turns        []Turn       `json:"turns"`
	NextCursor   string       `json:"next_cursor"`
	HasMore      bool         `json:"has_more"`
}
type ContextPage struct {
	Items       []any    `json:"items"`
	NextCursor  string   `json:"next_cursor,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
}
type ActionPayload struct {
	Phase          string   `json:"phase,omitempty"`
	ResolutionRefs []string `json:"resolution_refs,omitempty"`
}

type DecisionInput struct {
	Payload        *ActionPayload `json:"payload,omitempty"`
	RequestKey     string         `json:"request_key"`
	WorkRef        string         `json:"work_ref"`
	Instruction    string         `json:"instruction"`
	Scope          string         `json:"scope"`
	TargetRevision string         `json:"target_revision"`
	Origin         *Origin        `json:"origin,omitempty"`
}
type Decision struct {
	ProposedBy       string         `json:"proposed_by,omitempty"`
	OriginKind       string         `json:"origin_kind,omitempty"`
	TurnID           string         `json:"turn_id,omitempty"`
	SupersededBy     string         `json:"superseded_by,omitempty"`
	SupersededReason string         `json:"superseded_reason,omitempty"`
	CanAnswer        bool           `json:"can_answer"`
	Payload          *ActionPayload `json:"payload,omitempty"`
	ID               string         `json:"id"`
	WorkspaceID      string         `json:"workspace_id"`
	ActorID          string         `json:"actor_id"`
	WorkRef          string         `json:"work_ref"`
	Instruction      string         `json:"instruction"`
	Scope            string         `json:"scope"`
	TargetRevision   string         `json:"target_revision"`
	Status           Status         `json:"status"`
	Revision         int            `json:"revision"`
	Answer           string         `json:"answer,omitempty"`
	AnsweredBy       string         `json:"answered_by,omitempty"`
	ActionID         string         `json:"action_id,omitempty"`
	Origin           *Origin        `json:"origin,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}
type AnswerInput struct {
	Revision int    `json:"revision"`
	Approve  bool   `json:"approve"`
	Text     string `json:"text"`
}
type Action struct {
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
	Status                Status   `json:"status"`
	ExternalID            string   `json:"external_id,omitempty"`
	URL                   string   `json:"url,omitempty"`
	EvidenceRefs          []string `json:"evidence_refs,omitempty"`
	IndependentlyVerified bool     `json:"independently_verified"`
	Detail                string   `json:"detail,omitempty"`
}
type Delivery struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	ActorID     string         `json:"actor_id"`
	Origin      Origin         `json:"origin"`
	Text        string         `json:"text"`
	ReplyMarkup map[string]any `json:"reply_markup,omitempty"`
	Status      Status         `json:"status"`
	Receipt     Receipt        `json:"receipt"`
	Revision    int            `json:"revision"`
	Attempts    []Attempt      `json:"attempts"`
	NextRetryAt *time.Time     `json:"next_retry_at,omitempty"`
}

type Config struct {
	WorkspaceID    string
	WorkspaceName  string
	BaseURL        string
	AgentActorID   string
	AgentHandle    string
	TurnTimeout    time.Duration
	MaxOutputBytes int
	MaxConcurrent  int
}
type DispatchRequest struct {
	Turn           Turn
	Conversation   Conversation
	Packet         router.WakePacket
	MaxOutputBytes int
	Timeout        time.Duration
}

// Dependencies are trusted server wiring, never supplied by a request or model.
// Authorize must consult current principal permissions on EVERY operation.
// Execute must atomically enforce TargetRevision at the source if supported;
// otherwise it must fail closed when a race cannot be excluded. The action ID
// is the stable remote idempotency key. CheckDelivery is a required read-only
// preflight using the same routing as Execute. Missing executors must be rejected
// there, before recording any attempt. Errors after Execute starts are uncertain.
// Reconcile is read-only.
type Dependencies struct {
	ResolveResolution func(context.Context, Principal, string) (ResolutionRef, error)
	Authorize         func(context.Context, Principal, string, string) error
	EnsureThread      func(context.Context, Principal, string, string) (string, error)
	ReadContext       func(context.Context, Principal, string, string, int) (ContextPage, error)
	ReadContextPage   func(context.Context, Principal, string, string, string, int) (ContextPage, error)
	Dispatch          func(context.Context, DispatchRequest) error
	CurrentRevision   func(context.Context, Principal, string) (string, error)
	CheckDelivery     func(context.Context, Action) error
	Execute           func(context.Context, Action) (Receipt, error)
	Reconcile         func(context.Context, Action) (Receipt, error)
}
