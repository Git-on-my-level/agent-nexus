package primitives

import (
	"context"
	"slices"
	"strings"
	"unicode"
)

// askDuplicateScan bounds the open asks one requester's new ask is compared
// with. The scan reads idx_brief_asks_requester newest first, so the cost per
// filed ask is constant however many asks the workspace holds.
const askDuplicateScan = 64

// DuplicateAskError reports that the requester already has the same question
// open on the same card. Filing is redirected to that ask instead of adding a second row to the
// human's decisions.
type DuplicateAskError struct{ AskID string }

func (e *DuplicateAskError) Error() string { return "the same ask is already open: event:" + e.AskID }

// AskTitleKey reduces a title to the letters and digits a reader compares, so
// case, spacing, punctuation and invisible characters never make a repeat look
// new.
func AskTitleKey(title string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
			continue
		}
		space = true
	}
	return b.String()
}

// openDuplicateAskTx returns the newest open ask by the same requester, for
// the same recipient, about the same card, whose title reads the same. The
// card is part of the key because a generic title ("Approve this merge?") is a
// different question on a different card. Batch members and other
// request kinds (review, escalate, recover) are never matched: a batch is one
// decision item already and a recovery alarm keeps its own single row. The
// excluded ids are the ask being replaced and the incoming event itself, so a
// client retry of the same event still reaches the idempotent replay path.
func openDuplicateAskTx(ctx context.Context, tx *accessTx, requester, recipient, subject, title string, exclude ...string) (string, error) {
	key := AskTitleKey(title)
	if key == "" || strings.TrimSpace(requester) == "" {
		return "", nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT ask_id,title FROM brief_asks WHERE requester=? AND state='open' AND kind='ask' AND recipient=? AND subject_ref=? AND batch_id='' ORDER BY created_at DESC LIMIT ?`, requester, recipient, subject, askDuplicateScan)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, other string
		if err := rows.Scan(&id, &other); err != nil {
			return "", err
		}
		if !slices.Contains(exclude, id) && AskTitleKey(other) == key {
			return id, rows.Err()
		}
	}
	return "", rows.Err()
}
