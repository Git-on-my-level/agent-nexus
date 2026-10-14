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

	"agent-nexus-core/internal/resourceaccess"
)

func (s *Service) createConversationWithReplay(ctx context.Context, p Principal, in CreateConversation) (Conversation, error) {
	if in.ActorID != "" && in.ActorID != p.ActorID {
		return Conversation{}, ErrForbidden
	}
	if !validText(in.ReplayKey, 256) || strings.ContainsRune(in.ReplayKey, 0) || !validText(in.RequestKey, 256) || !validText(in.Title, 256) || len(in.WorkRef) > 512 {
		return Conversation{}, ErrInvalid
	}
	refs, err := conversationRefs(in.WorkRef, in.ContextRefs)
	if err != nil {
		return Conversation{}, err
	}
	// Raw authentication reads must finish before acquiring the single writer.
	// The scoped thread and PM record writes retain current resource admission.
	if err = s.authorize(ctx, p, "pm.read", in.WorkRef); err != nil {
		return Conversation{}, err
	}
	for _, ref := range refs {
		if err = s.authorize(ctx, p, "pm.read", ref); err != nil {
			return Conversation{}, err
		}
	}
	raw, err := json.Marshal(in) // Hash the complete decoded body before normalization.
	if err != nil {
		return Conversation{}, err
	}
	sum := sha256.Sum256(raw)
	r := proposalReplay{Scope: "pm.conversations.create:" + p.WorkspaceID, Actor: p.ActorID, Key: in.ReplayKey, Hash: hex.EncodeToString(sum[:])}
	if prior, found, err := s.store.readConversationReplay(ctx, r); err != nil || found {
		return prior, err
	}
	if err = s.RequireOnboarded(ctx); err != nil {
		return Conversation{}, err
	}
	if s.deps.CreateConversationThread == nil {
		return Conversation{}, ErrUnavailable
	}
	var out Conversation
	err = s.store.database().WriteTransaction(ctx, func(ctx context.Context, tx *resourceaccess.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope=? AND actor_id=? AND request_key=?`, r.Scope, r.Actor, r.Key); err != nil {
			return err
		}
		if prior, found, err := s.store.readConversationReplay(ctx, r); err != nil || found {
			out = prior
			return err
		}
		id := stableID("conversation", p.WorkspaceID, p.ActorID, in.RequestKey)
		var prior Conversation
		if err := s.store.get(ctx, "conversation", id, &prior); err == nil {
			return ErrConflict // No original receipt: never adopt legacy state.
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		thread, err := s.deps.CreateConversationThread(ctx, p, id, in.WorkRef)
		if err != nil {
			return err
		}
		if thread == "" {
			return ErrUnavailable
		}
		out = Conversation{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Title: in.Title, WorkRef: in.WorkRef, ContextRefs: refs, ThreadID: thread, CreatedAt: time.Now().UTC()}
		inserted, err := s.store.insert(ctx, "conversation", id, p.WorkspaceID, p.ActorID, "", out)
		if err != nil {
			return err
		}
		if !inserted {
			return ErrConflict
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES(?,?,?,?,?,?)`, r.Scope, r.Actor, r.Key, r.Hash, http.StatusCreated, string(raw)); err != nil {
			return err
		}
		out.ReplayJSON = raw
		return nil
	})
	return out, err
}

func (s *Store) readConversationReplay(ctx context.Context, r proposalReplay) (Conversation, bool, error) {
	var hash, raw string
	var status int
	err := s.database().QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, r.Scope, r.Actor, r.Key).Scan(&hash, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, false, nil
	}
	if err != nil {
		return Conversation{}, false, err
	}
	if hash != r.Hash {
		return Conversation{}, false, ErrConflict
	}
	var out Conversation
	if err = json.Unmarshal([]byte(raw), &out); err != nil {
		return Conversation{}, false, err
	}
	if status != http.StatusCreated || out.ID == "" {
		return Conversation{}, false, ErrInvalid
	}
	// Receipts are not an authority cache. The current scoped canonical record
	// must still be visible, including its private thread ownership boundary.
	var current Conversation
	if err = s.get(ctx, "conversation", out.ID, &current); err != nil {
		return Conversation{}, false, err
	}
	if current.ActorID != r.Actor || current.WorkspaceID != out.WorkspaceID {
		return Conversation{}, false, ErrForbidden
	}
	out.ReplayJSON = json.RawMessage(raw)
	return out, true, nil
}
