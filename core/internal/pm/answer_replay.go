package pm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"agent-nexus-core/internal/resourceaccess"
)

type answerReplay struct {
	Scope, Actor, Key, Hash string
	Response                Decision
}

func newAnswerReplay(p Principal, id string, in AnswerInput, response Decision) (*answerReplay, error) {
	if in.RequestKey == "" {
		return nil, nil
	}
	if !validText(in.RequestKey, 256) {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(struct {
		DecisionID string
		Answer     AnswerInput
	}{id, in})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	return &answerReplay{Scope: "pm.decisions.answer:" + p.WorkspaceID, Actor: p.ActorID, Key: in.RequestKey, Hash: hex.EncodeToString(hash[:]), Response: response}, nil
}

func (s *Store) readAnswerReplay(ctx context.Context, p Principal, id string, in AnswerInput) (Decision, bool, error) {
	r, err := newAnswerReplay(p, id, in, Decision{})
	if err != nil || r == nil {
		return Decision{}, false, err
	}
	var hash, raw string
	err = s.database().QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, r.Scope, r.Actor, r.Key).Scan(&hash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Decision{}, false, nil
	}
	if err != nil {
		return Decision{}, false, err
	}
	if hash != r.Hash {
		return Decision{}, false, ErrConflict
	}
	var result struct {
		Decision
		WorkMissing     bool  `json:"work_missing"`
		TargetCurrent   *bool `json:"target_current"`
		AlreadyAtTarget *bool `json:"already_at_target"`
	}
	err = json.Unmarshal([]byte(raw), &result)
	d := result.Decision
	d.WorkMissing, d.TargetCurrent, d.AlreadyAtTarget = result.WorkMissing, result.TargetCurrent, result.AlreadyAtTarget
	d.AnswerReplayJSON = json.RawMessage(raw)
	return d, err == nil, err
}

func writeAnswerReplayTx(ctx context.Context, tx *resourceaccess.Tx, r *answerReplay) error {
	if r == nil {
		return nil
	}
	raw := r.Response.AnswerReplayJSON
	if len(raw) == 0 {
		return ErrInvalid
	}
	inserted, err := tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES(?,?,?,?,200,?) ON CONFLICT(scope,actor_id,request_key) DO NOTHING`, r.Scope, r.Actor, r.Key, r.Hash, string(raw))
	if err != nil {
		return err
	}
	n, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// Bind a legacy revision-based acknowledgement to a newly supplied key without
// changing the decision or recreating its action. Verify the existing answer
// under the same write lock that claims the replay key.
func (s *Store) bindAnswerReplay(ctx context.Context, d Decision, r *answerReplay) error {
	if r == nil {
		return nil
	}
	tx, err := s.database().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE pm_records SET revision=revision WHERE kind='decision' AND id=?`, d.ID); err != nil {
		return err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind='decision' AND id=?`, d.ID).Scan(&raw); err != nil {
		return err
	}
	var current Decision
	if err = json.Unmarshal(raw, &current); err != nil {
		return err
	}
	if current.Revision != d.Revision || current.AnsweredBy != d.AnsweredBy || current.Answer != d.Answer || current.Status != d.Status {
		return ErrConflict
	}
	if err = writeAnswerReplayTx(ctx, tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) snapshotAnswerReplay(ctx context.Context, p Principal, id string, in AnswerInput, result Decision) (*answerReplay, error) {
	if in.RequestKey != "" {
		raw, err := json.Marshal(s.decisionResponse(ctx, p, result))
		if err != nil {
			return nil, err
		}
		result.AnswerReplayJSON = raw
	}
	return newAnswerReplay(p, id, in, result)
}
