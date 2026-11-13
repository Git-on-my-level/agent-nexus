package primitives

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// A standing ruling is an answer a human made durable. Two kinds exist:
//
//   - "answer": the human answered a question and said the answer always
//     holds. It is keyed by the exact title key and applies whoever asks.
//   - "not_a_decision": the human said this should not have reached them. It
//     is keyed by the title with every number collapsed, and by requester, so
//     the next dated copy of the same digest is told, not asked.
//
// Matching is deliberately exact. Whether a differently worded question is
// covered is a judgment for the asking agent, which reads the rulings list.
const (
	RulingKindAnswer        = "answer"
	RulingKindNotADecision  = "not_a_decision"
	StandingRulingListLimit = 200
	standingRulingAnswerMax = 2000
)

// StandingRuling is one row of standing_rulings.
type StandingRuling struct {
	ID, Kind, Question, Answer, Requester, SourceAskID, CreatedBy, CreatedAt string
	Uses                                                                     int
	LastUsedAt, RevokedAt, RevokedBy                                         string
}

// JSON renders the ruling for API responses.
func (r StandingRuling) JSON() map[string]any {
	out := map[string]any{"id": r.ID, "ref": "ruling:" + r.ID, "kind": r.Kind, "question": r.Question, "answer": r.Answer, "created_by": stripActor(r.CreatedBy), "created_at": r.CreatedAt, "uses": r.Uses, "active": r.RevokedAt == ""}
	if r.Requester != "" {
		out["requester"] = stripActor(r.Requester)
	}
	if r.SourceAskID != "" {
		out["source_ask_ref"] = "event:" + r.SourceAskID
	}
	if r.LastUsedAt != "" {
		out["last_used_at"] = r.LastUsedAt
	}
	if r.RevokedAt != "" {
		out["revoked_at"], out["revoked_by"] = r.RevokedAt, stripActor(r.RevokedBy)
	}
	return out
}

// RulingAppliedError reports that a standing ruling already covers the ask
// being filed. Nothing was stored; the caller answers with the ruling.
type RulingAppliedError struct{ Ruling StandingRuling }

func (e *RulingAppliedError) Error() string {
	return "a standing ruling covers this ask: ruling:" + e.Ruling.ID
}

// AskTitlePattern is AskTitleKey with every run of digits collapsed, so
// "Digest 2026-11-12" and "Digest 2026-11-13" are one recurring subject.
func AskTitlePattern(title string) string {
	var b strings.Builder
	digits := false
	for _, r := range AskTitleKey(title) {
		if unicode.IsDigit(r) {
			if !digits {
				b.WriteByte('#')
			}
			digits = true
			continue
		}
		digits = false
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// A ruling quotes the ask it was made from, so it is readable exactly where
// that ask is: every read joins the reader's scoped events relation. A member
// who cannot read the source ask neither lists the ruling nor has it applied.
const standingRulingColumns = `r.id,r.kind,r.question,r.answer,r.requester,r.source_ask_id,r.created_by,r.created_at,r.uses,COALESCE(r.last_used_at,''),COALESCE(r.revoked_at,''),r.revoked_by`

const standingRulingFrom = ` FROM standing_rulings r JOIN events e ON e.id=r.source_ask_id`

func scanStandingRuling(row interface{ Scan(...any) error }) (StandingRuling, error) {
	var r StandingRuling
	err := row.Scan(&r.ID, &r.Kind, &r.Question, &r.Answer, &r.Requester, &r.SourceAskID, &r.CreatedBy, &r.CreatedAt, &r.Uses, &r.LastUsedAt, &r.RevokedAt, &r.RevokedBy)
	return r, err
}

// CreateStandingRuling makes a human's answer to one ask durable. The ask
// supplies the question and, for not_a_decision, the requester it binds. A
// second ruling for the same key replaces the first: the older is revoked in
// the same transaction, so one question never has two live answers.
func (s *Store) CreateStandingRuling(ctx context.Context, actorID, askID, kind, answer string, now time.Time) (StandingRuling, error) {
	askID = strings.TrimPrefix(strings.TrimSpace(askID), "event:")
	answer = strings.TrimSpace(answer)
	if kind != RulingKindAnswer && kind != RulingKindNotADecision {
		return StandingRuling{}, workInvalid("kind must be answer or not_a_decision")
	}
	if kind == RulingKindAnswer && answer == "" {
		return StandingRuling{}, workInvalid("answer is required for an answer ruling")
	}
	if len(answer) > standingRulingAnswerMax {
		return StandingRuling{}, workInvalid("answer must be at most 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StandingRuling{}, err
	}
	defer tx.Rollback()
	var title, requester string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(json_extract(payload_json,'$.payload.title'),''),COALESCE(NULLIF(trim(json_extract(payload_json,'$.payload.requester_actor_id')),''),actor_id,'') FROM events WHERE id=? AND type='human_attention_requested'`, askID).Scan(&title, &requester)
	if errors.Is(err, sql.ErrNoRows) {
		return StandingRuling{}, ErrNotFound
	}
	if err != nil {
		return StandingRuling{}, err
	}
	r := StandingRuling{ID: "ruling_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Kind: kind, Question: strings.TrimSpace(title), Answer: answer, SourceAskID: askID, CreatedBy: actorID, CreatedAt: now.UTC().Format(time.RFC3339)}
	key := AskTitleKey(title)
	if kind == RulingKindNotADecision {
		key, r.Requester = AskTitlePattern(title), requester
	}
	if key == "" {
		return StandingRuling{}, workInvalid("the ask has no title to rule on")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE standing_rulings SET revoked_at=?,revoked_by=? WHERE title_key=? AND requester=? AND kind=? AND revoked_at IS NULL`, r.CreatedAt, actorID, key, r.Requester, kind); err != nil {
		return StandingRuling{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO standing_rulings(id,title_key,requester,kind,question,answer,source_ask_id,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, key, r.Requester, kind, r.Question, r.Answer, askID, actorID, r.CreatedAt); err != nil {
		return StandingRuling{}, err
	}
	return r, tx.Commit()
}

// StandingRulings lists rulings newest first, bounded by
// StandingRulingListLimit through idx_standing_rulings_recent.
func (s *Store) StandingRulings(ctx context.Context, includeRevoked bool) ([]StandingRuling, error) {
	where := ``
	if !includeRevoked {
		where = ` WHERE r.revoked_at IS NULL`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+standingRulingColumns+standingRulingFrom+where+` ORDER BY r.created_at DESC,r.id LIMIT ?`, StandingRulingListLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StandingRuling{}
	for rows.Next() {
		r, err := scanStandingRuling(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RevokeStandingRuling ends a ruling. Revoking twice is a no-op.
func (s *Store) RevokeStandingRuling(ctx context.Context, actorID, id string, now time.Time) (StandingRuling, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "ruling:")
	// Read through the scoped join first: a ruling the caller cannot see is
	// not found, and is not revoked.
	if _, err := scanStandingRuling(s.db.QueryRowContext(ctx, `SELECT `+standingRulingColumns+standingRulingFrom+` WHERE r.id=?`, id)); errors.Is(err, sql.ErrNoRows) {
		return StandingRuling{}, ErrNotFound
	} else if err != nil {
		return StandingRuling{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE standing_rulings SET revoked_at=?,revoked_by=? WHERE id=? AND revoked_at IS NULL`, now.UTC().Format(time.RFC3339), actorID, id); err != nil {
		return StandingRuling{}, err
	}
	r, err := scanStandingRuling(s.db.QueryRowContext(ctx, `SELECT `+standingRulingColumns+standingRulingFrom+` WHERE r.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return StandingRuling{}, ErrNotFound
	}
	return r, err
}

// RecordStandingRulingUse counts one ask the ruling answered.
func (s *Store) RecordStandingRulingUse(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE standing_rulings SET uses=uses+1,last_used_at=? WHERE id=?`, now.UTC().Format(time.RFC3339), id)
	return err
}

// standingRulingForAskTx finds the live ruling covering a new ask: an answer
// for the exact question, else a not_a_decision for this requester's pattern.
// Two index seeks on idx_standing_rulings_key.
func standingRulingForAskTx(ctx context.Context, tx *accessTx, requester, title string) (*StandingRuling, error) {
	key := AskTitleKey(title)
	if key == "" {
		return nil, nil
	}
	for _, q := range []struct {
		kind, key, requester string
	}{{RulingKindAnswer, key, ""}, {RulingKindNotADecision, AskTitlePattern(title), requester}} {
		r, err := scanStandingRuling(tx.QueryRowContext(ctx, `SELECT `+standingRulingColumns+standingRulingFrom+` WHERE r.title_key=? AND r.requester=? AND r.kind=? AND r.revoked_at IS NULL ORDER BY r.created_at DESC LIMIT 1`, q.key, q.requester, q.kind))
		if err == nil {
			return &r, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return nil, nil
}
