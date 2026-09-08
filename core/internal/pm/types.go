// Package pm owns durable conversational PM handoffs. Execution remains with the
// registered Nexus bridge; external source truth remains with its authority.
package pm

import (
	"context"
	"errors"
	"time"

	"agent-nexus-core/internal/router"
)

var (
	ErrForbidden   = errors.New("PM permission denied")
	ErrInvalid     = errors.New("invalid PM request")
	ErrConflict    = errors.New("PM revision or state conflict")
	ErrNotFound    = errors.New("PM record not found")
	ErrStale       = errors.New("approved source revision has changed")
	ErrUnavailable = errors.New("PM capability is not configured")
	ErrBusy        = errors.New("PM execution capacity reached")
)

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
type Turn struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	WorkspaceID    string    `json:"workspace_id"`
	ActorID        string    `json:"actor_id"`
	Text           string    `json:"text"`
	Response       string    `json:"response,omitempty"`
	Status         Status    `json:"status"`
	WakeupID       string    `json:"wakeup_id"`
	AgentActorID   string    `json:"agent_actor_id"`
	EvidenceRefs   []string  `json:"evidence_refs,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	Deadline       time.Time `json:"deadline"`
	Revision       int       `json:"revision"`
}
type ConversationDetail struct {
	Conversation Conversation `json:"conversation"`
	Turns        []Turn       `json:"turns"`
}
type ContextPage struct {
	Items       []any    `json:"items"`
	NextCursor  string   `json:"next_cursor,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
}
type DecisionInput struct {
	RequestKey     string  `json:"request_key"`
	WorkRef        string  `json:"work_ref"`
	Instruction    string  `json:"instruction"`
	Scope          string  `json:"scope"`
	TargetRevision string  `json:"target_revision"`
	Origin         *Origin `json:"origin,omitempty"`
}
type Decision struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	ActorID        string    `json:"actor_id"`
	WorkRef        string    `json:"work_ref"`
	Instruction    string    `json:"instruction"`
	Scope          string    `json:"scope"`
	TargetRevision string    `json:"target_revision"`
	Status         Status    `json:"status"`
	Revision       int       `json:"revision"`
	Answer         string    `json:"answer,omitempty"`
	AnsweredBy     string    `json:"answered_by,omitempty"`
	ActionID       string    `json:"action_id,omitempty"`
	Origin         *Origin   `json:"origin,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
type AnswerInput struct {
	Revision int    `json:"revision"`
	Approve  bool   `json:"approve"`
	Text     string `json:"text"`
}
type Action struct {
	ID                 string    `json:"id"`
	DecisionID         string    `json:"decision_id"`
	WorkspaceID        string    `json:"workspace_id"`
	ActorID            string    `json:"actor_id"`
	WorkRef            string    `json:"work_ref"`
	Instruction        string    `json:"instruction"`
	Scope              string    `json:"scope"`
	TargetRevision     string    `json:"target_revision"`
	AuthorizationBasis string    `json:"authorization_basis"`
	Status             Status    `json:"status"`
	Receipt            Receipt   `json:"receipt"`
	Attempts           []Attempt `json:"attempts"`
	Revision           int       `json:"revision"`
}
type Attempt struct {
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
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ActorID     string  `json:"actor_id"`
	Origin      Origin  `json:"origin"`
	Text        string  `json:"text"`
	Status      Status  `json:"status"`
	Receipt     Receipt `json:"receipt"`
	Revision    int     `json:"revision"`
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
// is the stable remote idempotency key. Reconcile is read-only.
type Dependencies struct {
	Authorize       func(context.Context, Principal, string, string) error
	EnsureThread    func(context.Context, Principal, string, string) (string, error)
	ReadContext     func(context.Context, Principal, string, string, int) (ContextPage, error)
	Dispatch        func(context.Context, DispatchRequest) error
	CurrentRevision func(context.Context, Principal, string) (string, error)
	Execute         func(context.Context, Action) (Receipt, error)
	Reconcile       func(context.Context, Action) (Receipt, error)
}
