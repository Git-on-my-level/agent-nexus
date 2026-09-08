package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidWorkRequest = errors.New("invalid work request")

func workInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidWorkRequest, fmt.Sprintf(format, args...))
}
func workString(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }
func workMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func workClone(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}
func workJSON(m map[string]any) string { b, _ := json.Marshal(m); return string(b) }
func workInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), n == float64(int64(n))
	case json.Number:
		i, e := n.Int64()
		return i, e == nil
	}
	return 0, false
}
func workPhase(p string) bool {
	switch p {
	case "backlog", "ready", "in_progress", "blocked", "review", "done", "cancelled", "unknown":
		return true
	}
	return false
}
func workTimestamp(v any) (time.Time, error) { return time.Parse(time.RFC3339Nano, workString(v)) }

// WorkListFilter filters the card-backed commitment projection. Cursor is opaque.
type WorkListFilter struct {
	ProjectRef, Source, Owner, Phase, Freshness, Query, Cursor string
	Limit                                                      int
}
type WorkPage struct {
	Work       []map[string]any `json:"work"`
	NextCursor string           `json:"next_cursor"`
}

func insertWorkMetadata(ctx context.Context, tx *sql.Tx, cardID, actorID string, m map[string]any) error {
	source := workMap(m["source"])
	_, err := tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,updated_at,updated_by) VALUES(?,?,?,?,?,?,?)`, cardID, workString(source["authority"]), workString(source["connection_id"]), workString(source["native_id"]), workJSON(m), time.Now().UTC().Format(time.RFC3339Nano), actorID)
	if err != nil {
		return err
	}
	var threadID, boardID string
	if err = tx.QueryRowContext(ctx, `SELECT thread_id,board_id FROM cards WHERE id=?`, cardID).Scan(&threadID, &boardID); err != nil {
		return err
	}
	return insertWorkEvent(ctx, tx, actorID, map[string]any{"id": cardID, "thread_id": threadID, "board_id": boardID}, "card_updated", "Commitment registered: "+workString(m["title"]), map[string]any{"changed_fields": []string{"work"}, "source": source})
}

func validateWorkLocal(m map[string]any) error {
	for _, k := range []string{"project_ref", "priority", "next_actor", "next_action", "wake_condition", "start_at", "due_at"} {
		if v, ok := m[k]; ok && v != nil {
			if _, ok := v.(string); !ok {
				return workInvalid("%s must be a string or null", k)
			}
		}
	}
	if p := workString(m["priority"]); p != "" && p != "p0" && p != "p1" && p != "p2" && p != "p3" {
		return workInvalid("invalid priority")
	}
	for _, k := range []string{"start_at", "due_at"} {
		if workString(m[k]) != "" {
			if _, err := workTimestamp(m[k]); err != nil {
				return workInvalid("%s must be RFC3339", k)
			}
		}
	}
	for _, k := range []string{"blockers", "relations", "executions"} {
		if v, ok := m[k]; ok {
			b, e := json.Marshal(v)
			if e != nil {
				return workInvalid("invalid %s", k)
			}
			var a []any
			if json.Unmarshal(b, &a) != nil || a == nil {
				return workInvalid("%s must be an array", k)
			}
			if len(a) > 200 {
				return workInvalid("%s exceeds 200 items", k)
			}
			for _, v := range a {
				if k == "blockers" {
					if _, ok := v.(string); !ok {
						return workInvalid("blockers must contain strings")
					}
				} else {
					item, ok := v.(map[string]any)
					if !ok {
						return workInvalid("%s must contain objects", k)
					}
					if k == "relations" {
						kind := workString(item["kind"])
						switch kind {
						case "parent", "child", "depends_on", "related", "artifact":
						default:
							return workInvalid("invalid relation kind")
						}
						if workString(item["ref"]) == "" {
							return workInvalid("relation ref required")
						}
					} else if workString(item["authority"]) == "" || workString(item["run_id"]) == "" {
						return workInvalid("execution authority and run_id required")
					}
				}
			}
		}
	}
	return nil
}

func (s *Store) CreateWork(ctx context.Context, actorID, boardID string, input map[string]any) (map[string]any, error) {
	if strings.TrimSpace(actorID) == "" {
		return nil, workInvalid("actor required")
	}
	m := workClone(input)
	delete(m, "actor_id")
	delete(m, "board_ref")
	if err := validateWorkLocal(m); err != nil {
		return nil, err
	}
	source := workMap(m["source"])
	authority := workString(source["authority"])
	if authority == "" {
		authority = "nexus"
	}
	source["authority"] = authority
	m["source"] = source
	if authority != "nexus" {
		if workString(source["connection_id"]) == "" || workString(source["native_id"]) == "" {
			return nil, workInvalid("external source requires connection_id and native_id")
		}
		var id string
		err := s.db.QueryRowContext(ctx, `SELECT card_id FROM work_metadata WHERE authority=? AND connection_id=? AND native_id=?`, authority, workString(source["connection_id"]), workString(source["native_id"])).Scan(&id)
		if err == nil {
			return s.GetWork(ctx, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	title := workString(m["title"])
	if title == "" {
		return nil, workInvalid("title required")
	}
	phase := workString(m["phase"])
	if phase == "" {
		phase = "backlog"
	}
	if !workPhase(phase) {
		return nil, workInvalid("invalid phase")
	}
	if phase == "done" {
		return nil, workInvalid("register active work, then submit completion evidence")
	}
	if authority == "nexus" {
		delete(m, "phase")
	} else {
		m["phase"] = phase
	}
	column := phase
	if column == "cancelled" || column == "unknown" {
		column = "backlog"
	}
	dod := []string{}
	if v, ok := m["definition_of_done"]; ok {
		b, _ := json.Marshal(v)
		if json.Unmarshal(b, &dod) != nil {
			return nil, workInvalid("definition_of_done must be strings")
		}
	}
	owner := workString(m["owner"])
	var assignee *string
	if owner != "" && authority == "nexus" {
		assignee = &owner
	}
	result, err := s.CreateBoardCard(ctx, actorID, boardID, AddBoardCardInput{Title: title, Body: workString(m["summary"]), ColumnKey: column, DefinitionOfDone: dod, Assignee: assignee, WorkMetadata: m})
	if err != nil {
		if authority != "nexus" {
			var id string
			if lookupErr := s.db.QueryRowContext(ctx, `SELECT card_id FROM work_metadata WHERE authority=? AND connection_id=? AND native_id=?`, authority, workString(source["connection_id"]), workString(source["native_id"])).Scan(&id); lookupErr == nil {
				return s.GetWork(ctx, id)
			}
		}
		return nil, err
	}
	return s.GetWork(ctx, workString(result.Card["id"]))
}

func (s *Store) workMetadata(ctx context.Context, id string) (map[string]any, int64, string, string, map[string]any, error) {
	var raw, latest, attempt, refresh string
	var version int64
	err := s.db.QueryRowContext(ctx, `SELECT metadata_json,version,COALESCE(latest_observation_id,''),COALESCE(latest_attempt_id,''),refresh_json FROM work_metadata WHERE card_id=?`, id).Scan(&raw, &version, &latest, &attempt, &refresh)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{"source": map[string]any{"authority": "nexus"}}, 0, "", "", map[string]any{"state": "idle"}, nil
	}
	if err != nil {
		return nil, 0, "", "", nil, err
	}
	var m, r map[string]any
	if err = json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, 0, "", "", nil, err
	}
	if err = json.Unmarshal([]byte(refresh), &r); err != nil {
		return nil, 0, "", "", nil, err
	}
	return m, version, latest, attempt, r, nil
}
func (s *Store) workObservation(ctx context.Context, id string) (map[string]any, error) {
	if id == "" {
		return nil, nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT body_json FROM work_observations WHERE id=?`, id).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = json.Unmarshal([]byte(raw), &m)
	return m, err
}

func (s *Store) GetWork(ctx context.Context, identifier string) (map[string]any, error) {
	card, err := s.getWorkCard(ctx, identifier)
	if err != nil {
		return nil, err
	}
	m, version, latestID, attemptID, refresh, err := s.workMetadata(ctx, workString(card["id"]))
	if err != nil {
		return nil, err
	}
	latest, err := s.workObservation(ctx, latestID)
	if err != nil {
		return nil, err
	}
	attempt, err := s.workObservation(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	source := workMap(m["source"])
	out := workClone(card)
	out["phase"] = card["column_key"]
	out["owner"] = ""
	if refs, ok := card["assignee_refs"].([]string); ok && len(refs) > 0 {
		out["owner"] = refs[0]
	}
	for _, key := range []string{"source", "project_ref", "priority", "next_actor", "next_action", "blockers", "wake_condition", "start_at", "due_at", "relations", "executions"} {
		if v, ok := m[key]; ok {
			out[key] = v
		}
	}
	for _, key := range []string{"blockers", "relations", "executions"} {
		if _, ok := out[key]; !ok {
			out[key] = []any{}
		}
	}
	if workString(source["authority"]) != "nexus" {
		for _, key := range []string{"title", "summary", "owner", "phase"} {
			if v, ok := m[key]; ok {
				out[key] = v
			}
		}
		if latest != nil {
			facts := workMap(latest["facts"])
			for _, key := range []string{"title", "summary", "owner", "phase"} {
				if v, ok := facts[key]; ok {
					out[key] = v
				}
			}
			for _, key := range []string{"native_status"} {
				if v, ok := facts[key]; ok {
					source[key] = v
				}
			}
			if v, ok := latest["source_revision"]; ok {
				source["revision"] = v
			}
		}
	}
	out["source"] = source
	out["version"] = version
	out["latest_observation"] = latest
	out["refresh"] = refresh
	fresh := map[string]any{"status": "unknown", "stale_after_seconds": 900}
	if latest != nil {
		for _, key := range []string{"source_activity_at", "meaningful_progress_at"} {
			if v, ok := latest[key]; ok {
				fresh[key] = v
			}
		}
		fresh["last_observed_at"] = latest["observed_at"]
		ttl := int64(900)
		if n, ok := workInt(latest["stale_after_seconds"]); ok {
			ttl = n
		}
		fresh["stale_after_seconds"] = ttl
		observed, e := workTimestamp(latest["observed_at"])
		if e == nil {
			fresh["status"] = "fresh"
			if time.Since(observed) > time.Duration(ttl)*time.Second {
				fresh["status"] = "stale"
			}
		}
	}
	if attempt != nil && workString(attempt["status"]) == "error" {
		fresh["status"] = "error"
		fresh["last_error"] = attempt["error"]
	}
	if refresh["state"] == "failed" {
		fresh["status"] = "error"
		fresh["last_error"] = refresh["last_error"]
	}
	out["freshness"] = fresh
	return out, nil
}

func (s *Store) ListWork(ctx context.Context, f WorkListFilter) (WorkPage, error) {
	page := WorkPage{Work: []map[string]any{}}
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Limit < 1 || f.Limit > 200 {
		return page, workInvalid("limit must be 1..200")
	}
	after := ""
	if f.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		if e != nil || len(b) == 0 {
			return page, ErrInvalidCursor
		}
		after = string(b)
	}
	cards, err := s.ListCards(ctx, CardListFilter{})
	if err != nil {
		return page, err
	}
	sort.Slice(cards, func(i, j int) bool { return workString(cards[i]["id"]) < workString(cards[j]["id"]) })
	for _, card := range cards {
		id := workString(card["id"])
		if id <= after {
			continue
		}
		w, err := s.GetWork(ctx, id)
		if err != nil {
			return page, err
		}
		if f.ProjectRef != "" && workString(w["project_ref"]) != f.ProjectRef {
			continue
		}
		if f.Source != "" && workString(workMap(w["source"])["authority"]) != f.Source {
			continue
		}
		if f.Owner != "" && workString(w["owner"]) != f.Owner {
			continue
		}
		if f.Phase != "" && workString(w["phase"]) != f.Phase {
			continue
		}
		if f.Freshness != "" && workString(workMap(w["freshness"])["status"]) != f.Freshness {
			continue
		}
		if f.Query != "" && !strings.Contains(strings.ToLower(workString(w["title"])+" "+workString(w["summary"])), strings.ToLower(f.Query)) {
			continue
		}
		if len(page.Work) == f.Limit {
			page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(workString(page.Work[len(page.Work)-1]["id"])))
			break
		}
		page.Work = append(page.Work, w)
	}
	return page, nil
}

func (s *Store) ensureWorkMetadata(ctx context.Context, id, actor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,'nexus','{"source":{"authority":"nexus"}}',0,?,?) ON CONFLICT(card_id) DO NOTHING`, id, time.Now().UTC().Format(time.RFC3339Nano), actor)
	return err
}
func (s *Store) PatchWork(ctx context.Context, actor, identifier string, version int64, patch map[string]any) (map[string]any, error) {
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	if len(patch) == 0 {
		return nil, workInvalid("patch required")
	}
	allowed := map[string]bool{}
	for _, key := range []string{"project_ref", "priority", "next_actor", "next_action", "blockers", "wake_condition", "start_at", "due_at", "relations", "executions"} {
		allowed[key] = true
	}
	for key := range patch {
		if !allowed[key] {
			return nil, workInvalid("%s is source-owned; use native card APIs or authorized source action", key)
		}
	}
	if err := validateWorkLocal(patch); err != nil {
		return nil, err
	}
	w, err := s.GetWork(ctx, identifier)
	if err != nil {
		return nil, err
	}
	id := workString(w["id"])
	if err = s.ensureWorkMetadata(ctx, id, actor); err != nil {
		return nil, err
	}
	m, v, _, _, _, err := s.workMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	if v != version {
		return nil, ErrConflict
	}
	for k, val := range patch {
		m[k] = val
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE work_metadata SET metadata_json=?,version=version+1,updated_at=?,updated_by=? WHERE card_id=? AND version=?`, workJSON(m), time.Now().UTC().Format(time.RFC3339Nano), actor, id, version)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrConflict
	}
	if err = insertWorkEvent(ctx, tx, actor, w, "card_updated", "Work annotations updated", map[string]any{"changed_fields": []string{"work_annotations"}, "patch": patch}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetWork(ctx, id)
}

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
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	o := workClone(input)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Acquire the SQLite write lock before reading replay/order state.
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET card_id=card_id WHERE card_id=?`, id); err != nil {
		return nil, err
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
			materialChange = workJSON(workMap(prior["facts"])) != workJSON(facts) || workString(prior["meaningful_progress_at"]) != workString(o["meaningful_progress_at"])
		}
	}
	if materialChange {
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
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET latest_observation_id=?,latest_attempt_id=?,refresh_json=?,version=version+1,updated_at=?,updated_by=? WHERE card_id=?`, oldGoodID, oldAttemptID, workJSON(refresh), o["received_at"], actor, id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	w, err := s.GetWork(ctx, id)
	return map[string]any{"observation": o, "work": w, "duplicate": false}, err
}

func (s *Store) RequestWorkRefresh(ctx context.Context, actor, identifier string) (map[string]any, error) {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
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

// ensureNativeWorkMutation prevents the legacy card surface from silently
// overriding an external system's source-owned state.
func ensureNativeWorkMutation(ctx context.Context, q queryRower, cardID string) error {
	var authority string
	err := q.QueryRowContext(ctx, `SELECT authority FROM work_metadata WHERE card_id=?`, cardID).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if authority != "nexus" {
		return invalidBoardRequest("external work is source-owned; request an authorized PM action")
	}
	return nil
}

// getWorkCard resolves the same public typed refs/handles as HTTP, without
// loading unbounded card revision history into portfolio queries.
func (s *Store) getWorkCard(ctx context.Context, identifier string) (map[string]any, error) {
	resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: identifier})
	if err != nil {
		return nil, err
	}
	row, err := s.loadBoardCardByGlobalID(ctx, s.db, resolved.ID, true)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}
