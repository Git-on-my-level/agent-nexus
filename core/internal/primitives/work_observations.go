// Work observation submission and refresh: validation, evidence completion
// refs, external card moves, and observation listing.

package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

func (s *Store) ListWorkObservations(ctx context.Context, identifier string, limit int, cursor string) ([]map[string]any, string, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 200 {
		return nil, "", workInvalid("limit must be 1..200")
	}
	card, err := s.getWorkCard(ctx, identifier)
	if err != nil {
		return nil, "", err
	}
	cardID := workString(card["id"])
	where := "card_id=?"
	args := []any{cardID}
	if cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return nil, "", ErrInvalidCursor
		}
		var parts []string
		if json.Unmarshal(b, &parts) != nil || len(parts) != 3 || parts[2] != cardID {
			return nil, "", ErrInvalidCursor
		}
		if _, e := workTimestamp(parts[0]); e != nil {
			return nil, "", ErrInvalidCursor
		}
		where += " AND (received_at<? OR (received_at=? AND id<?))"
		args = append(args, parts[0], parts[0], parts[1])
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT body_json FROM work_observations WHERE `+where+` ORDER BY received_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, "", err
		}
		var m map[string]any
		if err = json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, "", err
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		encoded, _ := json.Marshal([]string{workString(last["received_at"]), workString(last["id"]), cardID})
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return out, next, nil
}

func (s *Store) SubmitWorkObservation(ctx context.Context, actor, identifier string, input map[string]any) (map[string]any, error) {
	return s.submitWorkObservation(ctx, actor, identifier, "", input)
}

// SubmitLeasedWorkObservation fences the observation write itself, not merely
// the final refresh state, so a resumed expired worker cannot regress evidence.
func (s *Store) SubmitLeasedWorkObservation(ctx context.Context, actor, identifier, token string, input map[string]any) (map[string]any, error) {
	if token == "" {
		return nil, workInvalid("lease token required")
	}
	return s.submitWorkObservation(ctx, actor, identifier, token, input)
}

func (s *Store) submitWorkObservation(ctx context.Context, actor, identifier, token string, input map[string]any) (workResult map[string]any, workErr error) {
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	o := workClone(input)
	if err := validateObservationAliases(o); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "received_at", "actor_id", "verification", "work_ref"} {
		delete(o, key)
	}
	for _, key := range []string{"idempotency_key", "reader_id", "reader_revision", "observed_at", "status"} {
		if workString(o[key]) == "" {
			return nil, workInvalid("%s required", key)
		}
	}
	observed, err := workTimestamp(o["observed_at"])
	if err != nil {
		return nil, workInvalid("observed_at must be RFC3339")
	}
	now := time.Now().UTC()
	if observed.After(now.Add(5 * time.Minute)) {
		return nil, workInvalid("observed_at exceeds allowed clock skew")
	}
	o["observed_at"] = observed.UTC().Format(time.RFC3339Nano)
	status := workString(o["status"])
	switch status {
	case "reported", "verified", "uncertain", "error":
	default:
		return nil, workInvalid("invalid observation status")
	}
	for _, key := range []string{"source_activity_at", "meaningful_progress_at"} {
		if workString(o[key]) != "" {
			ts, e := workTimestamp(o[key])
			if e != nil || ts.After(observed.Add(5*time.Minute)) {
				return nil, workInvalid("invalid %s", key)
			}
		}
	}
	var sequence any
	if v, exists := o["source_sequence"]; exists {
		n, ok := workInt(v)
		if !ok || n < 0 {
			return nil, workInvalid("source_sequence must be a nonnegative integer")
		}
		sequence = n
	}
	if v, exists := o["stale_after_seconds"]; exists {
		n, ok := workInt(v)
		if !ok || n < 1 || n > 2592000 {
			return nil, workInvalid("stale_after_seconds must be 1..2592000")
		}
	}
	facts := workMap(o["facts"])
	if phase := workString(facts["phase"]); phase != "" && !workPhase(phase) {
		return nil, workInvalid("invalid normalized phase")
	}
	if workString(facts["phase"]) == "done" && status != "error" {
		b, _ := json.Marshal(o["evidence"])
		var evidence []map[string]any
		_ = json.Unmarshal(b, &evidence)
		hasRef := false
		for _, e := range evidence {
			if workString(e["ref"]) != "" || workString(e["url"]) != "" {
				hasRef = true
			}
		}
		if !hasRef {
			return nil, workInvalid("completion requires referenced evidence")
		}
	}
	card, err := s.getWorkCard(ctx, identifier)
	if err != nil {
		return nil, err
	}
	id := workString(card["id"])
	if err = s.ensureWorkMetadata(ctx, id, actor); err != nil {
		return nil, err
	}
	digestBytes := sha256.Sum256([]byte(workJSON(o)))
	digest := hex.EncodeToString(digestBytes[:])
	key := workString(o["idempotency_key"])
	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return nil, err
	}
	// The transaction now holds the workspace writer: from here any
	// failure leaves the outcome unknown unless it is a deterministic
	// request rejection.
	defer func() {
		if workErr != nil {
			workErr = mutationOutcomeAfterBegin(workErr)
		}
	}()
	defer tx.Rollback()
	// Acquire the SQLite write lock before reading replay/order state.
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET card_id=card_id WHERE card_id=?`, id); err != nil {
		return nil, err
	}
	if token != "" {
		var leaseRaw string
		if err = tx.QueryRowContext(ctx, `SELECT refresh_json FROM work_metadata WHERE card_id=?`, id).Scan(&leaseRaw); err != nil {
			return nil, err
		}
		var lease map[string]any
		if err = json.Unmarshal([]byte(leaseRaw), &lease); err != nil {
			return nil, err
		}
		expires, e := workTimestamp(lease["lease_expires_at"])
		if workString(lease["lease_token"]) != token || lease["state"] != "running" || e != nil || !expires.After(time.Now()) {
			return nil, ErrConflict
		}
	}
	var existingDigest, raw string
	err = tx.QueryRowContext(ctx, `SELECT digest,body_json FROM work_observations WHERE card_id=? AND idempotency_key=?`, id, key).Scan(&existingDigest, &raw)
	if err == nil {
		if existingDigest != digest {
			return nil, ErrConflict
		}
		var existing map[string]any
		_ = json.Unmarshal([]byte(raw), &existing)
		_ = tx.Rollback()
		w, e := s.GetWork(ctx, id)
		return map[string]any{"observation": existing, "work": w, "duplicate": true}, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	o["id"] = uuid.NewString()
	o["received_at"] = now.Format("2006-01-02T15:04:05.000000000Z")
	o["actor_id"] = actor
	o["work_ref"] = card["ref"]
	o["verification"] = "reported"
	var oldGoodID, oldAttemptID, refreshRaw string
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(latest_observation_id,''),COALESCE(latest_attempt_id,''),refresh_json FROM work_metadata WHERE card_id=?`, id).Scan(&oldGoodID, &oldAttemptID, &refreshRaw); err != nil {
		return nil, err
	}
	newer := func(oldID string) (bool, error) {
		if oldID == "" {
			return true, nil
		}
		var oldRaw string
		if e := tx.QueryRowContext(ctx, `SELECT body_json FROM work_observations WHERE id=?`, oldID).Scan(&oldRaw); e != nil {
			return false, e
		}
		var old map[string]any
		if e := json.Unmarshal([]byte(oldRaw), &old); e != nil {
			return false, e
		}
		oldSeq, oldHasSeq := workInt(old["source_sequence"])
		newSeq, newHasSeq := workInt(sequence)
		if oldHasSeq {
			if !newHasSeq || newSeq < oldSeq {
				return false, nil
			}
			if newSeq > oldSeq {
				return true, nil
			}
			// Equal source revision with equal facts is a new successful poll,
			// not progress. Conflicting same-revision facts cannot replace it.
			if workJSON(workMap(old["facts"])) != workJSON(facts) || workString(old["source_revision"]) != workString(o["source_revision"]) {
				return false, nil
			}
			oldTime, e := workTimestamp(old["observed_at"])
			return observed.After(oldTime), e
		}
		if newHasSeq {
			return true, nil
		}
		oldTime, e := workTimestamp(old["observed_at"])
		return observed.After(oldTime), e
	}
	goodNew, err := newer(oldGoodID)
	if err != nil {
		return nil, err
	}
	// Refresh-attempt health is ordered separately from source revisions: an
	// unreachable source cannot supply a sequence number, but its failed read
	// must still surface without replacing last-good facts.
	attemptNew := oldAttemptID == ""
	if oldAttemptID != "" {
		var oldObserved string
		if err = tx.QueryRowContext(ctx, `SELECT observed_at FROM work_observations WHERE id=?`, oldAttemptID).Scan(&oldObserved); err != nil {
			return nil, err
		}
		oldTime, parseErr := workTimestamp(oldObserved)
		if parseErr != nil {
			return nil, parseErr
		}
		attemptNew = observed.After(oldTime)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,source_sequence,status,body_json) VALUES(?,?,?,?,?,?,?,?,?)`, o["id"], id, key, digest, o["observed_at"], o["received_at"], sequence, status, workJSON(o)); err != nil {
		return nil, err
	}
	materialChange := false
	if goodNew && status != "error" {
		materialChange = oldGoodID == ""
		if oldGoodID != "" {
			var priorRaw string
			if err = tx.QueryRowContext(ctx, `SELECT body_json FROM work_observations WHERE id=?`, oldGoodID).Scan(&priorRaw); err != nil {
				return nil, err
			}
			var prior map[string]any
			if err = json.Unmarshal([]byte(priorRaw), &prior); err != nil {
				return nil, err
			}
			materialChange = workJSON(workMap(prior["facts"])) != workJSON(facts) || workString(prior["meaningful_progress_at"]) != workString(o["meaningful_progress_at"]) || sourceEvidenceChanged(prior["evidence"], o["evidence"])
		}
	}
	if materialChange {
		if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$._anx_source_evidence_at',?) WHERE card_id=?`, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
			return nil, err
		}
		if err = insertWorkEvent(ctx, tx, actor, card, "card_updated", "Source observation changed", map[string]any{"changed_fields": []string{"source_observation"}, "observation_id": o["id"], "source_revision": o["source_revision"], "knowledge": "reported", "facts": facts}); err != nil {
			return nil, err
		}
	}
	if attemptNew && status == "error" {
		previousStatus := ""
		if oldAttemptID != "" {
			if err = tx.QueryRowContext(ctx, `SELECT status FROM work_observations WHERE id=?`, oldAttemptID).Scan(&previousStatus); err != nil {
				return nil, err
			}
		}
		if previousStatus != "error" {
			if err = insertWorkEvent(ctx, tx, actor, card, "exception_raised", "Source refresh failed", map[string]any{"subtype": "source_refresh_failed", "observation_id": o["id"], "error": o["error"]}); err != nil {
				return nil, err
			}
		}
	}
	if goodNew && status != "error" {
		oldGoodID = workString(o["id"])
	}
	var refresh map[string]any
	_ = json.Unmarshal([]byte(refreshRaw), &refresh)
	if attemptNew {
		oldAttemptID = workString(o["id"])
		if refresh["state"] != "running" {
			refresh["last_attempt_at"] = o["received_at"]
		}
		if status == "error" {
			if refresh["state"] != "running" {
				refresh["state"] = "failed"
			}
			refresh["last_error"] = o["error"]
		} else {
			if refresh["state"] != "running" {
				refresh["state"] = "succeeded"
			}
			refresh["last_success_at"] = o["received_at"]
			delete(refresh, "last_error")
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET latest_observation_id=?,latest_attempt_id=?,refresh_json=?,updated_at=?,updated_by=? WHERE card_id=?`, oldGoodID, oldAttemptID, workJSON(refresh), o["received_at"], actor, id); err != nil {
		return nil, err
	}
	// Only the observation that becomes the latest good evidence may move the
	// card. Error, stale, and duplicate observations leave the board column.
	// The external source revision stays the decision fence: this does not
	// bump work_metadata.version.
	if goodNew && status != "error" {
		if err = s.moveExternalWorkForObservation(ctx, tx, actor, card, id, facts, o["evidence"]); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	w, err := s.GetWork(ctx, id)
	return map[string]any{"observation": o, "work": w, "duplicate": false}, err
}

func (s *Store) RequestWorkRefresh(ctx context.Context, actor, identifier string) (workResult map[string]any, workErr error) {
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	card, err := s.getWorkCard(ctx, identifier)
	if err != nil {
		return nil, err
	}
	id := workString(card["id"])
	if err = s.ensureWorkMetadata(ctx, id, actor); err != nil {
		return nil, err
	}
	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return nil, err
	}
	// The transaction now holds the workspace writer: from here any
	// failure leaves the outcome unknown unless it is a deterministic
	// request rejection.
	defer func() {
		if workErr != nil {
			workErr = mutationOutcomeAfterBegin(workErr)
		}
	}()
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET card_id=card_id WHERE card_id=?`, id); err != nil {
		return nil, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT refresh_json FROM work_metadata WHERE card_id=?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	var refresh map[string]any
	if err = json.Unmarshal([]byte(raw), &refresh); err != nil {
		return nil, err
	}
	if refresh["state"] != "queued" && refresh["state"] != "running" {
		refresh["state"] = "queued"
		refresh["requested_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		refresh["requested_by"] = actor
		if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET refresh_json=? WHERE card_id=?`, workJSON(refresh), id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return refresh, nil
}

// moveExternalWorkForObservation places an external card in the column named
// by the latest good observation. Native cards stay on the board-move API.
// cancelled and unknown follow creation and land in backlog unless that board
// actually has the named column. done carries the observation's evidence
// through the same completion gate as MoveBoardCard.
func (s *Store) moveExternalWorkForObservation(ctx context.Context, tx *accessTx, actor string, card map[string]any, cardID string, facts map[string]any, evidence any) error {
	phase := workString(facts["phase"])
	if phase == "" {
		return nil
	}
	var authority string
	if err := tx.QueryRowContext(ctx, `SELECT authority FROM work_metadata WHERE card_id=?`, cardID).Scan(&authority); err != nil {
		return err
	}
	if authority == "" || authority == "nexus" {
		return nil
	}
	boardID := workString(card["board_id"])
	column, err := observedWorkColumn(ctx, tx, boardID, phase)
	if err != nil {
		return err
	}
	if column == "" {
		return nil
	}
	cardRow, err := s.loadBoardCardByIdentifier(ctx, tx, boardID, cardID, true)
	if err != nil {
		return err
	}
	// An archived or trashed card keeps its column. The observation is still
	// accepted as evidence; a sync must not fail because a human archived it.
	if ensureBoardCardMutable(cardRow) != nil || cardRow.ColumnKey == column {
		return nil
	}
	fromColumn := cardRow.ColumnKey
	input := MoveBoardCardInput{ColumnKey: column}
	if column == "done" {
		refs, refErr := observationCompletionRefs(ctx, tx, actor, card, evidence)
		if refErr != nil {
			return refErr
		}
		if len(refs) == 0 {
			return workInvalid("completion requires referenced evidence")
		}
		done := "done"
		input.Resolution = &done
		input.ResolutionRefs = &refs
	}
	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, column, "", "", cardID)
	if err != nil {
		return err
	}
	nextResolution, nextResolutionRefsJSON, updateCard, err := resolveBoardCardMoveResolution(cardRow, column, input)
	if err != nil {
		return err
	}
	if column == "done" {
		var refs []string
		if err = json.Unmarshal([]byte(nextResolutionRefsJSON), &refs); err != nil {
			return err
		}
		if err = validateResolutionRefs(ctx, tx, refs); err != nil {
			return err
		}
	}
	if err = upsertBoardCardRefEdge(ctx, tx, boardID, cardID, column, rank); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if updateCard {
		if _, err = tx.ExecContext(ctx, `UPDATE cards SET column_key=?, rank=?, resolution=?, resolution_refs_json=?, updated_at=?, updated_by=? WHERE id=?`, column, rank, nullableString(nextResolution), nextResolutionRefsJSON, now, actor, cardID); err != nil {
			return err
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE cards SET column_key=?, rank=?, updated_at=?, updated_by=? WHERE id=?`, column, rank, now, actor, cardID); err != nil {
		return err
	}
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		return err
	}
	if _, err = touchBoardRow(ctx, tx, boardRow, actor); err != nil {
		return err
	}
	title := workString(card["title"])
	if title == "" {
		title = cardRow.Title
	}
	_, err = insertWorkEventRef(ctx, tx, actor, card, "card_moved", "Card moved: "+title, map[string]any{
		"title":            title,
		"summary":          workString(card["summary"]),
		"from_column_key":  fromColumn,
		"column_key":       column,
		"before_card_id":   nil,
		"after_card_id":    nil,
		"before_thread_id": nil,
		"after_thread_id":  nil,
	})
	return err
}

func observedWorkColumn(ctx context.Context, q queryRower, boardID, phase string) (string, error) {
	has, err := boardHasColumn(ctx, q, boardID, phase)
	if err != nil {
		return "", err
	}
	if has {
		return phase, nil
	}
	if phase == "cancelled" || phase == "unknown" {
		return "backlog", nil
	}
	if err = validateBoardColumnKey(phase); err != nil {
		return "", invalidBoardRequestError(err)
	}
	return phase, nil
}

func boardHasColumn(ctx context.Context, q queryRower, boardID, key string) (bool, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT COALESCE(column_schema_json,'') FROM boards WHERE id=?`, boardID).Scan(&raw)
	if err != nil {
		return false, err
	}
	items, err := decodeBoardColumnSchema(raw)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if workString(item["key"]) == key {
			return true, nil
		}
	}
	return false, nil
}

func observationEvidenceItems(evidence any) []map[string]any {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return nil
	}
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	return items
}

// observationCompletionRefs reuses live artifact or event refs from the
// observation. URL-only source evidence is recorded as a workspace event so
// the board done gate still has a resolution ref.
func observationCompletionRefs(ctx context.Context, tx *accessTx, actor string, card map[string]any, evidence any) ([]string, error) {
	items := observationEvidenceItems(evidence)
	var typed []string
	for _, item := range items {
		for _, key := range []string{"ref", "url"} {
			value := workString(item[key])
			prefix, _, ok := normalizeTypedRef(value)
			if !ok || (prefix != "artifact" && prefix != "event") {
				continue
			}
			typed = append(typed, value)
		}
	}
	typed = uniqueSortedStrings(typed)
	if len(typed) > 0 {
		if err := validateResolutionRefs(ctx, tx, typed); err == nil {
			return typed, nil
		}
	}
	if len(items) == 0 {
		return nil, nil
	}
	ref, err := insertWorkEventRef(ctx, tx, actor, card, "completion_evidence", "External completion evidence", map[string]any{
		"evidence":  items,
		"knowledge": "reported",
	})
	if err != nil {
		return nil, err
	}
	if ref == "" {
		return nil, workInvalid("completion requires referenced evidence")
	}
	return []string{ref}, nil
}
