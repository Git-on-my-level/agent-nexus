package pm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

func (s *Service) postMessageWithReplay(ctx context.Context, p Principal, conversationID string, in MessageInput) (Turn, error) {
	if in.ActorID != "" && in.ActorID != p.ActorID {
		return Turn{}, ErrForbidden
	}
	c, err := s.conversation(ctx, p, conversationID)
	if err != nil {
		return Turn{}, err
	}
	if !validText(in.Text, 16000) || !validText(in.RequestKey, 256) || !validText(in.ReplayKey, 256) || strings.ContainsRune(in.ReplayKey, 0) {
		return Turn{}, ErrInvalid
	}
	raw, err := json.Marshal(struct {
		ConversationID string `json:"conversation_id"`
		MessageInput
	}{conversationID, in})
	if err != nil {
		return Turn{}, err
	}
	sum := sha256.Sum256(raw)
	r := proposalReplay{Scope: "pm.messages.create:" + p.WorkspaceID, Actor: p.ActorID, Key: in.ReplayKey, Hash: hex.EncodeToString(sum[:])}
	if prior, found, err := s.store.readMessageReplay(ctx, r); err != nil || found {
		return prior, err
	}
	if err = s.RequireOnboarded(ctx); err != nil {
		return Turn{}, err
	}
	if s.deps.ReplayMessage == nil || (s.deps.Dispatch != nil && (s.deps.PreflightDispatch == nil || s.deps.PersistDispatch == nil || s.cfg.AgentHandle == "")) {
		return Turn{}, ErrUnavailable
	}
	agent := s.AgentActorID()
	if strings.TrimSpace(agent) == "" {
		return Turn{}, ErrPMIdentity
	}
	id := stableID("turn", c.ID, in.RequestKey)
	now := time.Now().UTC()
	t := Turn{ContextRefs: c.ContextRefs, ID: id, ConversationID: c.ID, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Text: in.Text, Status: Pending, WakeupID: stableID("wake", id), AgentActorID: agent, MaxOutputBytes: s.cfg.MaxOutputBytes, Origin: c.Origin, CreatedAt: now, Deadline: now.Add(s.cfg.TurnTimeout), Revision: 1}
	req := DispatchRequest{Turn: t, Conversation: c, Packet: s.packet(c, t), MaxOutputBytes: s.cfg.MaxOutputBytes, Timeout: s.cfg.TurnTimeout}
	req.Packet.ActorID = t.AgentActorID
	// Authentication/runtime readiness must not open a second connection while
	// the workspace writer is held. No durable turn is admitted on failure.
	if s.deps.Dispatch != nil {
		if err = s.deps.PreflightDispatch(ctx, req); err != nil {
			return Turn{}, err
		}
	}
	out, err := s.deps.ReplayMessage(ctx, r.Scope, r.Actor, r.Key, r.Hash, func(ctx context.Context) (Turn, error) {
		// Recheck scoped conversation admission under the write lock, without raw
		// authentication reads. Its current private refs must remain visible.
		var current Conversation
		if err := s.store.get(ctx, "conversation", c.ID, &current); err != nil {
			return Turn{}, err
		}
		if current.ActorID != p.ActorID || current.WorkspaceID != p.WorkspaceID || current.ThreadID != c.ThreadID || current.WorkRef != c.WorkRef || !sameConversationRefs(current, c.ContextRefs) || !sameOrigin(current.Origin, c.Origin) {
			return Turn{}, ErrConflict
		}
		inserted, err := s.store.insertTurn(ctx, t, s.cfg.MaxQueued)
		if err != nil {
			return Turn{}, err
		}
		if !inserted {
			return Turn{}, ErrConflict // Existing legacy turn has no original receipt.
		}
		t.Status, t.Revision = Sending, 2
		if err = s.store.cas(ctx, "turn", t.ID, 1, t); err != nil {
			return Turn{}, err
		}
		req.Turn = t
		if s.deps.Dispatch != nil {
			if err = s.deps.PersistDispatch(ctx, req); err != nil {
				return Turn{}, err
			}
		}
		return t, nil
	})
	if errors.Is(err, primitives.ErrConflict) {
		err = ErrConflict
	}
	if err == nil {
		// The quota-owning wrapper may have found a receipt from a concurrent
		// sender. Recheck current scoped turn visibility on that path too.
		var current Turn
		if readErr := s.store.get(ctx, "turn", out.ID, &current); readErr != nil {
			return Turn{}, readErr
		}
		if current.ActorID != p.ActorID || current.WorkspaceID != p.WorkspaceID {
			return Turn{}, ErrForbidden
		}
	}
	return out, err
}

// ReplayMessage owns quota locking before the writer; audited AppendEvent and
// CreateArtifact reuse that ownership. Queue writes and cold blob-ledger rebuild
// use savepoints. Blob promotion can leave an unreferenced content-addressed blob
// after rollback, but no canonical dispatch intent becomes visible before commit.
func (b NexusBridge) ReplayMessage(ctx context.Context, scope, actor, key, hash string, run func(context.Context) (Turn, error)) (Turn, error) {
	if b.Store == nil {
		return Turn{}, ErrUnavailable
	}
	receipt, err := b.Store.ExecuteWithReplay(ctx, scope, actor, key, hash, http.StatusAccepted, func(ctx context.Context) (map[string]any, error) {
		t, err := run(ctx)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(turnResponse(t, false))
		if err != nil {
			return nil, err
		}
		var out map[string]any
		err = json.Unmarshal(raw, &out)
		return out, err
	})
	if err != nil {
		return Turn{}, err
	}
	raw, err := json.Marshal(receipt.Response)
	if err != nil {
		return Turn{}, err
	}
	return decodeMessageReceipt(raw, receipt.Status)
}

func decodeMessageReceipt(raw []byte, status int) (Turn, error) {
	var out Turn
	if err := json.Unmarshal(raw, &out); err != nil {
		return Turn{}, err
	}
	if status != http.StatusAccepted || out.ID == "" {
		return Turn{}, ErrInvalid
	}
	out.ReplayJSON = raw
	return out, nil
}

func (s *Store) readMessageReplay(ctx context.Context, r proposalReplay) (Turn, bool, error) {
	var hash, raw string
	var status int
	err := s.database().QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, r.Scope, r.Actor, r.Key).Scan(&hash, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Turn{}, false, nil
	}
	if err != nil {
		return Turn{}, false, err
	}
	if hash != r.Hash {
		return Turn{}, false, ErrConflict
	}
	out, err := decodeMessageReceipt([]byte(raw), status)
	if err != nil {
		return Turn{}, false, err
	}
	var current Turn
	if err = s.get(ctx, "turn", out.ID, &current); err != nil {
		return Turn{}, false, err
	}
	if current.ActorID != r.Actor || current.WorkspaceID != out.WorkspaceID {
		return Turn{}, false, ErrForbidden
	}
	return out, true, nil
}
