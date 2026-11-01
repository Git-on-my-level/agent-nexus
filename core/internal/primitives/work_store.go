// Work item core: metadata insert and validation, default-board provisioning,
// CreateWork, and the canonical work projection.

package primitives

import (
	contractenums "agent-nexus-contracts-go-client/enums"
	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/plans"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
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

func insertWorkMetadata(ctx context.Context, tx *accessTx, cardID, actorID string, m map[string]any) error {
	source := workMap(m["source"])
	now := time.Now().UTC().Format(time.RFC3339Nano)
	delete(m, sourceEvidenceKey)
	delete(m, sourceEvidenceRefsKey)
	if sourceEvidenceChanged(nil, m["source_refs"]) {
		m[sourceEvidenceKey] = now
		m[sourceEvidenceRefsKey] = m["source_refs"]
	}
	if raw, ok := m["plan"]; ok {
		p, err := workCreatePlan(raw)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(p)
		if _, err = tx.ExecContext(ctx, `INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?)`, cardID, string(data), now); err != nil {
			return err
		}
		m["plan"] = p
	}
	if m["cadence"] != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE work_tracking SET default_at='' WHERE card_id=?`, cardID); err != nil {
			return err
		}
		m["cadence_set_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,updated_at,updated_by) VALUES(?,?,?,?,?,?,?)`, cardID, workString(source["authority"]), workString(source["connection_id"]), workString(source["native_id"]), workJSON(m), time.Now().UTC().Format(time.RFC3339Nano), actorID)
	if err != nil {
		return err
	}
	var threadID, boardID string
	if err = tx.QueryRowContext(ctx, `SELECT thread_id,board_id FROM cards WHERE id=?`, cardID).Scan(&threadID, &boardID); err != nil {
		return err
	}
	payload := map[string]any{"changed_fields": []string{"work"}, "source": source, "create_surface": m["create_surface"]}
	if move, ok := m["workspace_move"]; ok {
		payload["workspace_move"] = move
	}
	return insertWorkEvent(ctx, tx, actorID, map[string]any{"id": cardID, "thread_id": threadID, "board_id": boardID}, "card_updated", "Commitment registered: "+workString(m["title"]), payload)
}

// workLocalInvalid gives callers enough field-specific guidance to repair a value.
func workLocalInvalid(field, reason string) error {
	accepted := "a string or null"
	switch field {
	case "project_ref":
		accepted = "an existing workspace topic ref topic:<handle-or-id>, a bare topic handle or ID, an empty string, or null"
	case "priority":
		accepted = "p0, p1, p2, p3, an empty string, or null"
	case "start_at", "due_at":
		accepted = "an RFC 3339 timestamp, an empty string, or null"
	case "blockers":
		accepted = "an array of strings (at most 200 items)"
	case "relations":
		accepted = "an array of objects (at most 200 items) with kind (parent, child, depends_on, related, artifact) and a nonempty string ref <type>:<handle-or-id> resolving inside this workspace; parent, child, depends_on require card refs and also accept bare card handles or IDs"
	case "executions":
		accepted = "an array of objects (at most 200 items) with required nonempty string fields authority and run_id"
	}
	return workInvalid("%s: %s; accepted: %s", field, reason, accepted)
}

func validateWorkLocal(m map[string]any) error {
	if v, ok := m["top_level"]; ok {
		if _, valid := v.(bool); !valid {
			return workInvalid("top_level must be a boolean")
		}
	}
	if err := validateLoopMetadata(m); err != nil {
		return err
	}
	for _, key := range []string{"labels", "roles"} {
		if raw, exists := m[key]; exists && raw != nil {
			values, err := normalizeStringSlice(raw)
			if err != nil || len(values) > 16 {
				return workInvalid("%s must contain at most 16 strings", key)
			}
			for _, value := range values {
				if len(value) > 128 || strings.TrimSpace(value) == "" {
					return workInvalid("invalid %s value", key)
				}
			}
		}
	}

	if err := validateIndexedAliases(workMap(m["source"]), "source"); err != nil {
		return err
	}
	if raw, ok := m["source_refs"]; ok {
		if err := validateSourceRefs(raw); err != nil {
			return err
		}
	}
	for _, k := range []string{"project_ref", "priority", "next_actor", "next_action", "wake_condition", "start_at", "due_at", "risk"} {
		if v, ok := m[k]; ok && v != nil {
			if _, ok := v.(string); !ok {
				return workLocalInvalid(k, "must be a string or null")
			}
		}
	}
	if p := workString(m["priority"]); p != "" && !contractenums.IsPriority(p) {
		return workLocalInvalid("priority", "invalid priority")
	}
	if risk := workString(m["risk"]); risk != "" && risk != "low" && risk != "medium" && risk != "high" && risk != "critical" {
		return workLocalInvalid("risk", "invalid risk")
	}
	for _, k := range []string{"start_at", "due_at"} {
		if workString(m[k]) != "" {
			if _, err := workTimestamp(m[k]); err != nil {
				return workLocalInvalid(k, "invalid timestamp")
			}
		}
	}
	for _, k := range []string{"blockers", "relations", "executions"} {
		if v, ok := m[k]; ok {
			b, e := json.Marshal(v)
			if e != nil {
				return workLocalInvalid(k, "invalid value")
			}
			var a []any
			if json.Unmarshal(b, &a) != nil || a == nil {
				return workLocalInvalid(k, "must be an array")
			}
			if len(a) > 200 {
				return workLocalInvalid(k, "exceeds 200 items")
			}
			for _, v := range a {
				if k == "blockers" {
					if _, ok := v.(string); !ok {
						return workLocalInvalid(k, "must contain strings")
					}
				} else {
					item, ok := v.(map[string]any)
					if !ok {
						return workLocalInvalid(k, "must contain objects")
					}
					if k == "relations" {
						kind := workString(item["kind"])
						switch kind {
						case "parent", "child", "depends_on", "related", "artifact", "merged_into":
						default:
							return workLocalInvalid(k, "invalid relation kind")
						}
						if workString(item["ref"]) == "" {
							return workLocalInvalid(k, "relation ref required")
						}
					} else if workString(item["authority"]) == "" || workString(item["run_id"]) == "" {
						return workLocalInvalid(k, "execution authority and run_id required")
					}
				}
			}
		}
	}
	return nil
}

const (
	defaultWorkBoardID    = "workspace-default"
	defaultWorkBoardTitle = "Tasks"
)

func (s *Store) oldestActiveBoardID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM boards WHERE archived_at IS NULL AND trashed_at IS NULL ORDER BY created_at ASC, id ASC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(id), nil
}

// ensureDefaultBoard returns an active board for work.create when the caller
// omitted board_ref: the oldest active board, or a "Tasks" board created on
// the spot. Concurrent callers race here, and the reserved id is what makes
// that safe -- the loser of the insert gets ErrConflict and re-reads rather
// than creating a second board. The archived case is why it re-reads instead
// of just fetching by id: `workspace-default` can exist but be archived, and
// then the workspace genuinely needs a new board with a generated id.
func (s *Store) ensureDefaultBoard(ctx context.Context, actorID string) (string, error) {
	id, err := s.oldestActiveBoardID(ctx)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	board, err := s.CreateBoard(ctx, actorID, map[string]any{
		"id":    defaultWorkBoardID,
		"title": defaultWorkBoardTitle,
	})
	if err == nil {
		return workString(board["id"]), nil
	}
	if !errors.Is(err, ErrConflict) {
		return "", err
	}
	if id, readErr := s.oldestActiveBoardID(ctx); readErr != nil {
		return "", readErr
	} else if id != "" {
		return id, nil
	}
	// The reserved id is taken by an archived or trashed board, so this
	// workspace needs a fresh active one.
	board, err = s.CreateBoard(ctx, actorID, map[string]any{"title": defaultWorkBoardTitle})
	if err != nil {
		return "", err
	}
	return workString(board["id"]), nil
}

func (s *Store) CreateWork(ctx context.Context, actorID, boardID string, input map[string]any) (map[string]any, error) {
	if strings.TrimSpace(actorID) == "" {
		return nil, workInvalid("actor required")
	}
	boardID = strings.TrimSpace(boardID)
	m := workClone(input)
	for _, key := range []string{"under", "after"} {
		if value, exists := input[key]; exists {
			if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" {
				return nil, workInvalid("%s must be a nonempty string", key)
			}
		}
	}
	under, after := workString(m["under"]), workString(m["after"])
	if value, exists := input["initiative"]; exists {
		if _, ok := value.(bool); !ok {
			return nil, workInvalid("initiative must be a boolean")
		}
	}
	delete(m, "under")
	delete(m, "after")
	delete(m, "initiative")
	delete(m, "create_surface")
	delete(m, foldSnapshotKey)
	delete(m, "actor_id")
	delete(m, "request_key")
	delete(m, "board_ref")
	delete(m, sourceEvidenceKey)
	delete(m, sourceEvidenceRefsKey)
	if raw, ok := m["plan"]; ok {
		p, err := workCreatePlan(raw)
		if err != nil {
			return nil, err
		}
		m["plan"] = p
	}
	if err := validateWorkLocal(m); err != nil {
		return nil, err
	}
	if err := s.validateWorkReferences(ctx, m); err != nil {
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
			if under != "" {
				return s.GroupWork(ctx, actorID, id, under, after)
			}
			return s.GetWork(ctx, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	id := workString(m["id"])
	moveID := workString(workMap(m["workspace_move"])["move_id"])
	delete(m, "id")
	if value, present := m["handle"]; present {
		h, ok := value.(string)
		if !ok || !handles.ValidSlug(h) {
			return nil, workInvalid("handle must be a canonical lowercase slug of 1-64 characters, excluding reserved names and UUIDs")
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
	// Resolve the board last. Everything above can still reject the request or
	// return an existing card, and a rejected work.create must not leave a
	// default board behind on a workspace that has none.
	if boardID == "" {
		if under != "" {
			parent, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: under})
			if err != nil {
				return nil, err
			}
			if err = s.db.QueryRowContext(ctx, `SELECT board_id FROM cards WHERE id=?`, parent.ID).Scan(&boardID); err != nil {
				return nil, err
			}
			if boardID == "" {
				return nil, workInvalid("parent must belong to an active board")
			}
			board, err := s.getBoardRow(ctx, boardID)
			if err != nil {
				return nil, err
			}
			if board.Role == "initiatives" && !board.ArchivedAt.Valid && !board.TrashedAt.Valid {
				return s.createImplicitInitiativeChild(ctx, actorID, parent.ID, input)
			}

		} else {
			resolved, err := s.ensureDefaultBoard(ctx, actorID)
			if err != nil {
				return nil, err
			}
			boardID = resolved
		}
	}
	refs := []string{}
	if topicRef := workString(m["topic_ref"]); topicRef != "" {
		refs = append(refs, topicRef)
	}
	if related, err := optionalStringListField(m, "related_refs"); err == nil {
		refs = append(refs, related...)
	}
	var dueAt *string
	if raw := workString(m["due_at"]); raw != "" {
		dueAt = &raw
	}
	var pinnedDocumentID *string
	if documentRef := workString(m["document_ref"]); documentRef != "" {
		resolved, resolveErr := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "document", Ref: documentRef})
		if resolveErr != nil {
			return nil, workLocalInvalid("document_ref", "must resolve to an existing workspace document")
		}
		pinnedDocumentID = &resolved.ID
	}
	var risk *string
	if raw := workString(m["risk"]); raw != "" {
		risk = &raw
	}
	m["create_surface"] = firstNonEmptyString(workString(input["create_surface"]), "work.create")
	result, err := s.CreateBoardCard(ctx, actorID, boardID, AddBoardCardInput{
		Under: under, After: after, CreateSurface: firstNonEmptyString(workString(input["create_surface"]), "work.create"),
		// Loops (cadence or goal) are standing top-level commitments.
		Initiative: input["initiative"] == true || m["top_level"] == true || m["cadence"] != nil || m["goal"] != nil,
		Handle:     workString(m["handle"]), CardID: id, Title: title, Body: workString(m["summary"]), ColumnKey: column,
		DefinitionOfDone: dod, Assignee: assignee, DueAt: dueAt, PinnedDocumentID: pinnedDocumentID,
		Risk: risk, Refs: uniqueSortedStrings(refs), WorkMetadata: m,
	})
	if err != nil {
		// Caller-selected ids make native work creation replay-safe for a
		// workspace move. Reuse only when the persisted move marker proves this
		// is the same operation; an unrelated id collision remains a conflict.
		if errors.Is(err, ErrConflict) && authority == "nexus" && id != "" && moveID != "" {
			if existing, getErr := s.GetWork(ctx, id); getErr == nil && workString(workMap(existing["workspace_move"])["move_id"]) == moveID {
				return existing, nil
			}
		}
		if authority != "nexus" {
			var existing string
			if lookupErr := s.db.QueryRowContext(ctx, `SELECT card_id FROM work_metadata WHERE authority=? AND connection_id=? AND native_id=?`, authority, workString(source["connection_id"]), workString(source["native_id"])).Scan(&existing); lookupErr == nil {
				if under != "" {
					return s.GroupWork(ctx, actorID, existing, under, after)
				}
				return s.GetWork(ctx, existing)
			}
		}
		return nil, err
	}
	work, err := s.GetWork(ctx, workString(result.Card["id"]))
	if err != nil {
		return nil, err
	}
	work["likely_home"] = result.Card["likely_home"]
	work["parent_ref"] = result.Card["parent_ref"]
	return work, nil
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
	return projectWork(card, m, version, latest, attempt, refresh), nil
}

// projectWork shares canonical projection logic with bulk work and report reads.
// It performs no I/O.
func projectWork(card, m map[string]any, version int64, latest, attempt, refresh map[string]any) map[string]any {
	source := workMap(m["source"])
	out := workClone(card)
	out["phase"] = card["column_key"]
	out["owner"] = ""
	if refs, ok := card["assignee_refs"].([]string); ok && len(refs) > 0 {
		out["owner"] = refs[0]
	}
	for _, key := range []string{"top_level", "cadence", "goal", "source", "project_ref", "priority", "next_actor", "next_action", "next_action_at", "blockers", "wake_condition", "start_at", "due_at", "relations", "executions", "workspace_move", "topic_ref", "document_ref", "related_refs", "risk", "plan", "labels", "roles", "source_refs"} {
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
	for _, key := range []string{"labels", "roles"} {
		if out[key] == nil {
			delete(out, key)
		}
	}
	out["source"] = source
	out["version"] = version
	out["decision_revision"] = WorkDecisionRevision(out)
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
	return out
}

// ListAllWork uses two bulk reads regardless of card count. Projection rules are
// shared with GetWork so observations, freshness and source overlays stay equal.
func (s *Store) ListAllWork(ctx context.Context) ([]map[string]any, error) {
	cards, err := s.ListCards(ctx, CardListFilter{})
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, COALESCE(wm.metadata_json, '{"source":{"authority":"nexus"}}'),
        COALESCE(wm.version, 0), COALESCE(wm.refresh_json, '{"state":"idle"}'),
        COALESCE(latest.body_json, 'null'), COALESCE(attempt.body_json, 'null')
        FROM cards c `+cardVisibilityJoins+`
        LEFT JOIN work_observations latest ON latest.id = wm.latest_observation_id
        LEFT JOIN work_observations attempt ON attempt.id = wm.latest_attempt_id
        WHERE `+cardLifecycleWhere([]string{"active"}))
	if err != nil {
		return nil, err
	}
	type metadata struct {
		body, refresh, latest, attempt map[string]any
		version                        int64
	}
	byID := map[string]metadata{}
	for rows.Next() {
		var id, body, refresh, latest, attempt string
		var m metadata
		if err = rows.Scan(&id, &body, &m.version, &refresh, &latest, &attempt); err != nil {
			rows.Close()
			return nil, err
		}
		for _, value := range []struct {
			raw    string
			target *map[string]any
		}{
			{body, &m.body}, {refresh, &m.refresh}, {latest, &m.latest}, {attempt, &m.attempt},
		} {
			if err = json.Unmarshal([]byte(value.raw), value.target); err != nil {
				rows.Close()
				return nil, err
			}
		}
		byID[id] = m
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	work := make([]map[string]any, 0, len(cards))
	updated := map[string]time.Time{}
	for _, card := range cards {
		id := workString(card["id"])
		m, ok := byID[id]
		if !ok {
			continue
		} // archived concurrently between the two reads
		at, err := workTimestamp(card["updated_at"])
		if err != nil {
			return nil, err
		}
		updated[id] = at
		work = append(work, projectWork(card, m.body, m.version, m.latest, m.attempt, m.refresh))
	}
	sort.Slice(work, func(i, j int) bool {
		left, right := workString(work[i]["id"]), workString(work[j]["id"])
		if updated[left].Equal(updated[right]) {
			return left > right
		}
		return updated[left].After(updated[right])
	})
	return work, nil
}

// workCreatePlan validates before board creation; the same value is installed
// inside the card transaction, rather than left as an inert annotation.
func workCreatePlan(raw any) (plans.Plan, error) {
	var p plans.Plan
	data, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(data, &p)
	}
	if err == nil {
		err = plans.Validate(p)
	}
	if err != nil {
		return p, workInvalid("plan: %s; use anx plan set <card> --from-file <path> for a later plan", err)
	}
	for i := range p.Steps {
		if p.Steps[i].After == nil {
			p.Steps[i].After = []string{}
		}
	}
	return p, nil
}

func (s *Store) ensureWorkMetadata(ctx context.Context, id, actor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,'nexus','{"source":{"authority":"nexus"}}',0,?,?) ON CONFLICT(card_id) DO NOTHING`, id, time.Now().UTC().Format(time.RFC3339Nano), actor)
	return err
}
