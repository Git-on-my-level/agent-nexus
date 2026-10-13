// Package commentindex normalizes one trigger-captured source inside its
// caller's canonical transaction. It never returns source content to a reader.
package commentindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"agent-nexus-core/internal/scopesearch"
)

type Transaction interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func Normalize(ctx context.Context, tx Transaction, id string) error {
	var raw []byte
	var truncated bool
	err := tx.QueryRowContext(ctx, `SELECT raw_text,raw_truncated FROM comment_search_heads WHERE event_id=? AND ready=0`, id).Scan(&raw, &truncated)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	body, err := Prepare(raw, truncated)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE comment_search_heads SET projection_json=?,ready=1 WHERE event_id=? AND ready=0`, body, id)
	return err
}

// Prepare does bounded CPU work without needing a database transaction.
func Prepare(raw []byte, truncated bool) (string, error) {
	projection := scopesearch.PrepareComment(string(raw))
	projection.Truncated = projection.Truncated || truncated
	body, err := json.Marshal(projection)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// AfterWrite catches ordinary parameterized core event mutations in the same
// transaction. External and bulk literal writes remain queued for the worker.
// At most 64 distinct bound source identities are probed; no queue/history scan
// is added to the request. The trigger is the authority for source capture.
func AfterWrite(ctx context.Context, tx *sql.Tx, query string, args []any) error {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') })
	eventWrite := false
	for _, word := range words {
		if word == "events" {
			eventWrite = true
			break
		}
	}
	if !eventWrite {
		return nil
	}
	seen := map[string]bool{}
	for _, arg := range args {
		id, ok := arg.(string)
		if !ok || len(id) > 512 || seen[id] {
			continue
		}
		seen[id] = true
		if err := Normalize(ctx, tx, id); err != nil {
			return err
		}
		if len(seen) == 64 {
			break
		}
	}
	return nil
}
