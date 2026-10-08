package primitives

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/secrets"
	"github.com/google/uuid"
)

type AskSubscriptionInput struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

func WithAskWebhookEncryption(enc *secrets.Encryptor) Option {
	return func(s *Store) { s.askWebhookEncryption = enc }
}

func (s *Store) CreateAskSubscription(ctx context.Context, actor, askRef string, in AskSubscriptionInput) (map[string]any, error) {
	if actor == "" {
		return nil, ErrForbidden
	}
	if in.Kind != "await" && in.Kind != "bridge" && in.Kind != "webhook" {
		return nil, workInvalid("subscription kind must be await, bridge or webhook")
	}
	if strings.TrimSpace(in.Label) == "" || len(in.Label) > 120 {
		return nil, workInvalid("non-secret label must contain 1 to 120 bytes")
	}
	var secret string
	var ciphertext, nonce []byte
	if in.Kind == "webhook" {
		if s.askWebhookEncryption == nil {
			return nil, workInvalid("webhooks require configured ANX_SECRETS_KEY")
		}
		if err := ValidateAskWebhookURL(in.URL, s.askWebhookAllowHosts); err != nil {
			return nil, err
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		secret = hex.EncodeToString(b)
		var err error
		ciphertext, nonce, err = s.askWebhookEncryption.Encrypt([]byte(secret))
		if err != nil {
			return nil, err
		}
	} else if in.URL != "" {
		return nil, workInvalid("only webhook subscriptions accept a URL")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	askID := ""
	if askRef != "" {
		resolved, e := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "event", Ref: askRef})
		if e != nil {
			return nil, e
		}
		askID = resolved.ID
		var requester string
		if e = tx.QueryRowContext(ctx, `SELECT json_extract(payload_json,'$.payload.requester_actor_id') FROM events WHERE id=? AND type='human_attention_requested'`, askID).Scan(&requester); e != nil {
			return nil, ErrNotFound
		}
		if requester != actor {
			return nil, ErrForbidden
		}
	} else if in.Kind != "webhook" {
		return nil, workInvalid("standing subscriptions require webhook kind")
	}
	if in.Kind != "webhook" {
		var existing, label string
		err = tx.QueryRowContext(ctx, `SELECT id,label FROM ask_subscriptions WHERE actor_id=? AND ask_id=? AND kind=? AND enabled=1 LIMIT 1`, actor, askID, in.Kind).Scan(&existing, &label)
		if err == nil {
			return map[string]any{"id": existing, "kind": in.Kind, "label": label}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	var count int
	// Per-ask and per-agent caps compose to a maximum of 32 deliveries per ask.
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM ask_subscriptions WHERE actor_id=? AND ask_id=? AND enabled=1 LIMIT 16)`, actor, askID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= 16 {
		return nil, workInvalid("subscription limit reached (16)")
	}
	id := "sub_" + uuid.NewString()
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO ask_subscriptions(id,actor_id,ask_id,kind,label,endpoint,secret,nonce,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, actor, askID, in.Kind, in.Label, in.URL, ciphertext, nonce, at); err != nil {
		return nil, err
	}
	if askID != "" {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,kind,next_at) SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM human_attention_request_resolutions WHERE request_event_id=?)`, id+"."+askID, id, askID, in.Kind, at, askID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	out := map[string]any{"id": id, "kind": in.Kind, "label": in.Label, "state": "none", "attempts": 0}
	if secret != "" {
		out["secret"] = secret
	}
	return out, nil
}
func (s *Store) askDeliveries(ctx context.Context, id string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.kind,s.label,d.state,d.attempts,d.last_at,d.reason,d.status_code,d.latency_ms FROM ask_deliveries d JOIN ask_subscriptions s ON s.id=d.subscription_id WHERE d.ask_id=? ORDER BY d.id LIMIT 32`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, kind, label, state, at, reason string
		var attempts, code, latency int
		if err = rows.Scan(&id, &kind, &label, &state, &attempts, &at, &reason, &code, &latency); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "kind": kind, "label": label, "state": state, "attempts": attempts, "last_at": at, "reason": reason, "status_code": code, "latency_ms": latency})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	logs, err := s.db.QueryContext(ctx, `SELECT d.subscription_id,a.at,a.status_code,a.latency_ms,a.attempt FROM ask_deliveries d JOIN ask_delivery_attempts a ON a.delivery_id=d.id WHERE d.ask_id=? ORDER BY d.id,a.attempt LIMIT 160`, id)
	if err != nil {
		return nil, err
	}
	defer logs.Close()
	byID := map[string]map[string]any{}
	for _, item := range out {
		item["log"] = []map[string]any{}
		byID[anyStringValue(item["id"])] = item
	}
	for logs.Next() {
		var subscription, at string
		var code, latency, attempt int
		if err = logs.Scan(&subscription, &at, &code, &latency, &attempt); err != nil {
			return nil, err
		}
		if item := byID[subscription]; item != nil {
			item["log"] = append(item["log"].([]map[string]any), map[string]any{"at": at, "status_code": code, "latency_ms": latency, "attempt": attempt})
		}
	}
	return out, logs.Err()
}
func (s *Store) RecordAskDelivery(ctx context.Context, actor, subscription, askRef, state, reason string, attempts int) error {
	if state != "delivered" && state != "failed" && state != "pending" {
		return workInvalid("invalid delivery state")
	}
	if len(reason) > 200 || attempts < 0 || attempts > 20 {
		return workInvalid("invalid delivery receipt")
	}
	ref, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "event", Ref: askRef})
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE ask_deliveries SET state=?,reason=?,attempts=?,last_at=? WHERE subscription_id=? AND ask_id=? AND EXISTS(SELECT 1 FROM ask_subscriptions s WHERE s.id=? AND s.actor_id=? AND s.kind IN ('bridge','await'))`, state, reason, attempts, time.Now().UTC().Format(time.RFC3339Nano), subscription, ref.ID, subscription, actor)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("%w: delivery", ErrNotFound)
	}
	return nil
}
