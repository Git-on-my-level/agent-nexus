package primitives

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Store) OperatingRulingPage(ctx context.Context, cursor string) ([]OperatingRuling, string, string, error) {
	ctx, closeRead, err := s.BeginResourceRead(ctx)
	if err != nil {
		return nil, "", "", err
	}
	defer closeRead()
	var revision, accessVersion int64
	var key []byte
	err = s.db.QueryRowContext(ctx, `SELECT version,(SELECT version FROM resource_access_epoch WHERE singleton=1),cursor_key FROM standing_ruling_revision WHERE singleton=1`).Scan(&revision, &accessVersion, &key)
	if err != nil {
		return nil, "", "", err
	}
	codec, err := operatingRulingCursorCodec(key)
	if err != nil {
		return nil, "", "", err
	}
	after, err := decodeOperatingRulingCursor(codec, cursor)
	if err != nil {
		return nil, "", "", err
	}
	ids, err := s.operatingRulingPositions(ctx, `WHERE ruling_id>? ORDER BY ruling_id LIMIT ?`, after, OperatingRulingsPageSize+1)
	if err != nil {
		return nil, "", "", err
	}
	next := ""
	if len(ids) > OperatingRulingsPageSize {
		ids = ids[:OperatingRulingsPageSize]
		// The position for a bounded scan may itself be invisible to the
		// reader. An authenticated opaque cursor carries it without its ID.
		nonce := make([]byte, codec.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return nil, "", "", err
		}
		next = base64.RawURLEncoding.EncodeToString(codec.Seal(nonce, nonce, []byte(ids[len(ids)-1]), nil))
	}
	raw, _ := json.Marshal(ids)
	rulings, err := s.readOperatingRulings(ctx, `WHERE ruling_id IN (SELECT value FROM json_each(?)) ORDER BY ruling_id`, string(raw))
	return rulings, fmt.Sprintf("%d.%d", revision, accessVersion), next, err
}

func operatingRulingCursorCodec(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func decodeOperatingRulingCursor(codec cipher.AEAD, cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	if len(cursor) != base64.RawURLEncoding.EncodedLen(codec.NonceSize()+codec.Overhead()+len("R-20261127-01")) {
		return "", workInvalid("invalid ruling projection cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", workInvalid("invalid ruling projection cursor")
	}
	plain, err := codec.Open(nil, data[:codec.NonceSize()], data[codec.NonceSize():], nil)
	if err != nil || !ValidOperatingRulingID(string(plain)) {
		return "", workInvalid("invalid ruling projection cursor")
	}
	return string(plain), nil
}

func (s *Store) operatingRulingPositions(ctx context.Context, suffix string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ruling_id FROM operating_ruling_positions `+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) readOperatingRulings(ctx context.Context, suffix string, args ...any) ([]OperatingRuling, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+operatingRulingColumns+` FROM standing_rulings `+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OperatingRuling{}
	for rows.Next() {
		r, err := scanOperatingRuling(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OperatingRulingDecisions projects pending rows directly. A proposal and
// its decision are the same durable record, with no second inbox lifecycle.
func (s *Store) OperatingRulingDecisions(ctx context.Context, now time.Time) ([]map[string]any, bool, error) {
	rulings, err := s.readOperatingRulings(ctx, `WHERE status='proposed' AND ruling_id IS NOT NULL ORDER BY created_at DESC,ruling_id LIMIT ?`, BriefSectionLimit+1)
	if err != nil {
		return nil, false, err
	}
	partial := len(rulings) > BriefSectionLimit
	if partial {
		rulings = rulings[:BriefSectionLimit]
	}
	items := make([]map[string]any, 0, len(rulings))
	for _, r := range rulings {
		created, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		author := stripActor(r.CreatedBy)
		items = append(items, map[string]any{
			"ref": "ruling:" + r.RulingID, "inbox_item_id": "ruling:" + r.RulingID, "kind": "ruling_proposal",
			"title": r.Title, "body": r.Body, "subject": map[string]any{"ref": "ruling:" + r.RulingID},
			"requested_by": map[string]any{"id": author, "display": author},
			"created_at":   r.CreatedAt, "age_seconds": int64(now.Sub(created) / time.Second),
			"decide_by": nil, "overdue_for_seconds": nil, "blocks": []map[string]any{}, "blocks_count": 0,
			"proposals": []string{"Confirm ruling"}, "ruling_proposal": r.JSON(),
		})
	}
	return items, partial, nil
}
