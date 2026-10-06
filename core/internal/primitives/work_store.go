package primitives

import (
	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/schema"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
	Visible                                                    func(string, string) bool `json:"-"`
}
type WorkPage struct {
	Work       []map[string]any `json:"work"`
	NextCursor string           `json:"next_cursor"`
}

func insertWorkMetadata(ctx context.Context, tx *accessTx, cardID, actorID string, m map[string]any) error {
	source := workMap(m["source"])
	_, err := tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,updated_at,updated_by) VALUES(?,?,?,?,?,?,?)`, cardID, workString(source["authority"]), workString(source["connection_id"]), workString(source["native_id"]), workJSON(m), time.Now().UTC().Format(time.RFC3339Nano), actorID)
	if err != nil {
		return err
	}
	var threadID, boardID string
	if err = tx.QueryRowContext(ctx, `SELECT thread_id,board_id FROM cards WHERE id=?`, cardID).Scan(&threadID, &boardID); err != nil {
		return err
	}
	payload := map[string]any{"changed_fields": []string{"work"}, "source": source}
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
	if p := workString(m["priority"]); p != "" && p != "p0" && p != "p1" && p != "p2" && p != "p3" {
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
						case "parent", "child", "depends_on", "related", "artifact":
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
	delete(m, "actor_id")
	delete(m, "board_ref")
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
			return s.GetWork(ctx, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	id := workString(m["id"])
	moveID := workString(workMap(m["workspace_move"])["move_id"])
	delete(m, "id")
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
		resolved, err := s.ensureDefaultBoard(ctx, actorID)
		if err != nil {
			return nil, err
		}
		boardID = resolved
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
	result, err := s.CreateBoardCard(ctx, actorID, boardID, AddBoardCardInput{
		CardID: id, Title: title, Body: workString(m["summary"]), ColumnKey: column,
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
				return s.GetWork(ctx, existing)
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
	for _, key := range []string{"source", "project_ref", "priority", "next_actor", "next_action", "blockers", "wake_condition", "start_at", "due_at", "relations", "executions", "workspace_move", "topic_ref", "document_ref", "related_refs", "risk", "plan", "labels", "roles", "source_refs"} {
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

// ReportWorkFilter selects a bounded candidate set before projection. BoardIDs
// are resolved IDs, not authored SQL or handles. Each scope needs only one read.
type ReportWorkFilter struct {
	BoardIDs                []string
	CardID                  string
	ProjectRef              string
	Limit                   int
	IncludeClosed           bool
	OverviewClosed          *bool
	WorkList                *WorkListFilter
	BeforeUpdated, BeforeID string
	CardIDs                 []string
	CardLifecycleOnly       bool
	skipOwnerContext        bool
}
type ReportWorkBoard struct {
	Title, ThreadID, PrivateOwner, Role string
}

type ReportWorkPage struct {
	Work      []map[string]any
	Truncated bool
	// Request-local context from the same batch; never serialized in API work rows.
	Boards        map[string]ReportWorkBoard
	PrivateOwners map[string]string
}

// JSON null is a present override in projectWork, distinct from a missing key.
func projectedWorkStringSQL(key, canonical string) string {
	value := func(body, path string) string {
		return `CASE WHEN json_type(` + body + `,'` + path + `')='text' THEN anx_unicode_trim(json_extract(` + body + `,'` + path + `')) ELSE '' END`
	}
	return `CASE WHEN anx_unicode_trim(COALESCE(m.authority,'nexus'))='nexus' THEN ` + canonical + ` WHEN json_type(o.body_json,'$.facts.` + key + `') IS NOT NULL THEN ` + value("o.body_json", "$.facts."+key) + ` WHEN json_type(m.metadata_json,'$.` + key + `') IS NOT NULL THEN ` + value("m.metadata_json", "$."+key) + ` ELSE ` + canonical + ` END`
}

func reportWorkQuery(ctx context.Context, filter ReportWorkFilter) (string, []any) {
	return reportWorkSQL(filter, false)
}

func reportWorkSQL(filter ReportWorkFilter, candidatesOnly bool) (string, []any) {
	limit := filter.Limit
	if limit < 1 || limit > 2000 {
		limit = 2000
	}
	from := `cards c LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`
	where := strings.ReplaceAll(cardLifecycleWhere([]string{"active"}), "wm.metadata_json", "m.metadata_json")
	if filter.CardLifecycleOnly {
		where = `COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''`
	}
	args := []any{}
	if filter.ProjectRef != "" {
		// Lead with the expression index, avoiding a workspace-wide card read for
		// project-scoped panels. CROSS JOIN preserves this indexed join order.
		from = `work_metadata m INDEXED BY idx_work_metadata_project CROSS JOIN cards c ON c.id=m.card_id LEFT JOIN boards b ON b.id=c.board_id`
		where += ` AND json_extract(m.metadata_json,'$.project_ref')=?`
		args = append(args, filter.ProjectRef)
	}
	if len(filter.BoardIDs) > 0 {
		where += ` AND c.board_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filter.BoardIDs)), ",") + `)`
		for _, id := range filter.BoardIDs {
			args = append(args, id)
		}
	}
	if len(filter.CardIDs) > 0 {
		where += ` AND c.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(filter.CardIDs)), ",") + `)`
		for _, id := range filter.CardIDs {
			args = append(args, id)
		}
	}
	if filter.CardID != "" {
		where += ` AND c.id=?`
		args = append(args, filter.CardID)
	}
	// Match GetWork's source-authority rule, including the latest observation.
	from += ` LEFT JOIN work_observations o ON o.id=m.latest_observation_id
	 LEFT JOIN work_observations a ON a.id=m.latest_attempt_id`
	candidateFrom := from
	if filter.WorkList == nil || filter.WorkList.Freshness == "" {
		// Only the freshness selector needs attempt data before LIMIT.
		candidateFrom = strings.Replace(candidateFrom, ` LEFT JOIN work_observations a ON a.id=m.latest_attempt_id`, "", 1)
	}
	if filter.ProjectRef != "" {
		from = strings.Replace(from, `work_metadata m INDEXED BY idx_work_metadata_project CROSS JOIN cards c ON c.id=m.card_id LEFT JOIN boards b ON b.id=c.board_id`, `cards c LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`, 1)
	}
	ownerColumns := `COALESCE(json_extract(bt.body_json,'$.pm_actor_id'),''),COALESCE(json_extract(ct.body_json,'$.pm_actor_id'),'')`
	if filter.skipOwnerContext {
		ownerColumns = `'', ''`
	} else {
		from += ` LEFT JOIN threads ct ON ct.id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id))
	 LEFT JOIN threads bt ON bt.id=trim(b.thread_id)`
	}
	from += ` LEFT JOIN ref_edges placement ON placement.source_type='board' AND placement.target_type='card' AND placement.edge_type='board_card' AND placement.source_id=b.id AND placement.target_id=c.id`
	if !filter.IncludeClosed {
		where += ` AND (` + projectedWorkStringSQL("phase", "c.column_key") + `) NOT IN ('done','cancelled')`
	}
	beforeClosedWhere := where
	if filter.OverviewClosed != nil {
		where += ` AND (` + projectedWorkStringSQL("phase", "c.column_key") + ` IN ('done','cancelled'))=?`
		args = append(args, *filter.OverviewClosed)
	}
	if f := filter.WorkList; f != nil {
		if filter.BeforeID != "" {
			where += ` AND (anx_timestamp_key(c.updated_at),c.id)<(?,?)`
			args = append(args, filter.BeforeUpdated, filter.BeforeID)
		}
		phase := projectedWorkStringSQL("phase", "c.column_key")
		owner := projectedWorkStringSQL("owner", `CASE WHEN COALESCE(anx_unicode_trim(c.assignee),'')='' THEN '' WHEN instr(anx_unicode_trim(c.assignee),':')>0 THEN anx_unicode_trim(c.assignee) ELSE 'actor:'||anx_unicode_trim(c.assignee) END`)
		title := projectedWorkStringSQL("title", "c.title")
		summary := projectedWorkStringSQL("summary", "c.summary")
		freshness := `CASE WHEN json_extract(m.refresh_json,'$.state')='failed' OR json_extract(a.body_json,'$.status')='error' THEN 'error' WHEN julianday(json_extract(o.body_json,'$.observed_at')) IS NULL THEN 'unknown' WHEN (julianday('now')-julianday(json_extract(o.body_json,'$.observed_at')))*86400>COALESCE(json_extract(o.body_json,'$.stale_after_seconds'),900) THEN 'stale' ELSE 'fresh' END`
		for _, condition := range []struct{ expr, value string }{{`anx_unicode_trim(COALESCE(m.authority,'nexus'))`, f.Source}, {owner, f.Owner}, {phase, f.Phase}, {freshness, f.Freshness}} {
			if condition.value != "" {
				where += ` AND (` + condition.expr + `)=?`
				args = append(args, condition.value)
			}
		}
		if f.Query != "" {
			where += ` AND instr(anx_unicode_lower((` + title + `)||' '||(` + summary + `)),?)>0`
			args = append(args, strings.ToLower(f.Query))
		}
	}
	args = append(args, limit+1)
	ordering := ` ORDER BY anx_timestamp_key(c.updated_at) DESC,c.id DESC`
	if filter.WorkList == nil && filter.OverviewClosed == nil && !filter.IncludeClosed {
		ordering = ""
	}
	if filter.WorkList != nil {
		from = strings.ReplaceAll(from, ` LEFT JOIN ref_edges placement ON placement.source_type='board' AND placement.target_type='card' AND placement.edge_type='board_card' AND placement.source_id=b.id AND placement.target_id=c.id`, ``)
	}
	// Reports sort only bounded candidates; Overview also orders candidate
	// selection so its snapshots remain stable as closed history grows.
	placementColumns := `COALESCE(json_extract(placement.metadata_json,'$.column_key'),c.column_key),COALESCE(json_extract(placement.metadata_json,'$.rank'),c.rank)`
	if filter.WorkList != nil {
		placementColumns = `c.column_key,c.rank`
	}
	contextColumns := `,b.id AS board_id,b.handle AS board_handle,b.title AS board_title,b.role AS board_role,b.thread_id AS board_thread,
 m.metadata_json AS metadata_json,m.version AS metadata_version,m.refresh_json AS refresh_json,m.latest_attempt_id AS latest_attempt_id,o.body_json AS observation_json`
	prefix := `WITH _work_candidates AS MATERIALIZED (SELECT c.id` + contextColumns + ` FROM ` + candidateFrom + ` WHERE ` + where + ordering + ` LIMIT ?) `
	carryContext := true
	if filter.OverviewClosed != nil && *filter.OverviewClosed && filter.IncludeClosed && filter.WorkList == nil {
		carryContext = false
		// Native closed work can seek by its canonical phase. External phases
		// still use the full projection rule, but start from sparse metadata so
		// an empty closed-history page does not scan every active native card.
		native := func(phase string) string {
			nativeFrom := strings.SplitN(candidateFrom, ` LEFT JOIN work_observations`, 2)[0]
			return `SELECT c.id,anx_timestamp_key(c.updated_at) AS updated FROM ` + nativeFrom + ` WHERE ` + beforeClosedWhere + ` AND c.column_key='` + phase + `' AND anx_unicode_trim(COALESCE(m.authority,'nexus'))='nexus'` + ordering + ` LIMIT ?`
		}
		externalFrom := strings.Replace(candidateFrom, `cards c LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id`, `_external_work m CROSS JOIN cards c ON c.id=m.card_id LEFT JOIN boards b ON b.id=c.board_id`, 1)
		externalFrom = strings.Replace(externalFrom, `work_metadata m INDEXED BY idx_work_metadata_project`, `_external_work m`, 1)
		external := `SELECT c.id,anx_timestamp_key(c.updated_at) AS updated FROM ` + externalFrom + ` WHERE ` + where + ` AND anx_unicode_trim(COALESCE(m.authority,'nexus'))<>'nexus'` + ordering + ` LIMIT ?`
		prefix = `WITH _external_work AS MATERIALIZED (SELECT * FROM work_metadata WHERE anx_unicode_trim(COALESCE(authority,'nexus'))<>'nexus'), _work_candidates AS MATERIALIZED (SELECT id FROM (SELECT * FROM (` + native("done") + `) UNION ALL SELECT * FROM (` + native("cancelled") + `) UNION ALL SELECT * FROM (` + external + `)) ORDER BY updated DESC,id DESC LIMIT ?) `
		externalArgs := append([]any{}, args...)
		// Native selectors replace the projected-phase boolean with an indexed
		// canonical phase, so they do not bind the closed-phase parameter.
		nativeArgs := append(append([]any{}, args[:len(args)-2]...), limit+1)
		args = append(append(append(nativeArgs, nativeArgs...), externalArgs...), limit+1)
	}
	if candidatesOnly {
		return prefix + `SELECT id FROM _work_candidates`, args
	}
	projectionWhere := `c.id=selected.id`
	projectionLimit := ""
	if len(filter.CardIDs) > 0 {
		// Explicit IDs are already a bounded selector. Apply lifecycle and
		// optional filters directly, without repeating every scoped relation
		// in another candidate CTE before the same projection joins.
		prefix, projectionWhere, projectionLimit = "", where, ` LIMIT ?`
		carryContext = false
	} else {
		from = `_work_candidates selected CROSS JOIN ` + from
	}
	if carryContext {
		// These relations were already scoped and read in the candidate
		// statement snapshot. Reuse their bounded context during enrichment.
		for _, join := range []string{` LEFT JOIN boards b ON b.id=c.board_id`, ` LEFT JOIN work_metadata m ON m.card_id=c.id`, ` LEFT JOIN work_observations o ON o.id=m.latest_observation_id`} {
			from = strings.Replace(from, join, "", 1)
		}
	}
	projection := `SELECT COALESCE(b.id,''),b.handle,c.id,c.handle,` + placementColumns + `,
	 c.title,c.summary,c.version,c.head_revision_id,c.head_revision_number,c.thread_id,c.parent_thread_id,c.due_at,c.definition_of_done_json,
	 c.pinned_document_id,c.assignee,c.risk,c.resolution,c.resolution_refs_json,c.refs_json,c.created_at,c.created_by,c.updated_at,c.updated_by,c.provenance_json,
	 c.archived_at,c.archived_by,c.trashed_at,c.trashed_by,c.trash_reason,
	 COALESCE(m.metadata_json,'{"source":{"authority":"nexus"}}'),COALESCE(m.version,0),COALESCE(m.refresh_json,'{"state":"idle"}'),o.body_json,a.body_json,
	 COALESCE(b.title,''),COALESCE(b.role,''),COALESCE(b.thread_id,''),` + ownerColumns + `
	 FROM ` + from + ` WHERE ` + projectionWhere + ordering + projectionLimit
	if carryContext {
		projection = strings.NewReplacer("b.id", "selected.board_id", "b.handle", "selected.board_handle", "b.title", "selected.board_title", "b.role", "selected.board_role", "b.thread_id", "selected.board_thread", "m.metadata_json", "selected.metadata_json", "m.version", "selected.metadata_version", "m.refresh_json", "selected.refresh_json", "m.latest_attempt_id", "selected.latest_attempt_id", "o.body_json", "selected.observation_json").Replace(projection)
	}
	return prefix + projection, args
}

func (s *Store) ListReportWork(ctx context.Context, filter ReportWorkFilter) (ReportWorkPage, error) {
	if filter.OverviewClosed != nil && *filter.OverviewClosed && filter.IncludeClosed && filter.WorkList == nil {
		// This indexed, scoped superset cannot miss a closed candidate. Most
		// workspaces have neither canonical closed cards nor external overlays;
		// avoid compiling all lifecycle joins when both sets are empty.
		var possible bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE column_key IN ('done','cancelled') LIMIT 1) OR EXISTS(SELECT 1 FROM work_metadata WHERE anx_unicode_trim(COALESCE(authority,'nexus'))<>'nexus' LIMIT 1)`).Scan(&possible); err != nil {
			return ReportWorkPage{}, err
		}
		if !possible {
			return ReportWorkPage{Work: []map[string]any{}, Boards: map[string]ReportWorkBoard{}, PrivateOwners: map[string]string{}}, nil
		}
		// Avoid preparing the wide projection (and its reference/identity
		// predicates) for an empty closed-history page. The selector still
		// applies canonical visibility and lifecycle before its bounded limit.
		query, args := reportWorkSQL(filter, true)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return ReportWorkPage{}, err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return ReportWorkPage{}, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ReportWorkPage{}, err
		}
		if len(ids) == 0 {
			return ReportWorkPage{Work: []map[string]any{}, Boards: map[string]ReportWorkBoard{}, PrivateOwners: map[string]string{}}, nil
		}
		limit := filter.Limit
		if limit < 1 || limit > 2000 {
			limit = 2000
		}
		truncated := len(ids) > limit
		if truncated {
			ids = ids[:limit]
		}
		filter.CardIDs, filter.OverviewClosed = ids, nil
		page, err := s.ListReportWork(ctx, filter)
		page.Truncated = page.Truncated || truncated
		return page, err
	}
	query, args := reportWorkQuery(ctx, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return ReportWorkPage{}, err
	}
	defer rows.Close()
	limit := filter.Limit
	if limit < 1 || limit > 2000 {
		limit = 2000
	}
	page := ReportWorkPage{Work: []map[string]any{}, Boards: map[string]ReportWorkBoard{}, PrivateOwners: map[string]string{}}
	for rows.Next() {
		if len(page.Work) == limit {
			page.Truncated = true
			break
		}
		var metadataJSON, refreshJSON, privateOwner string
		var latestJSON, attemptJSON sql.NullString
		var version int64
		var board ReportWorkBoard
		row, err := scanBoardCardRow(rows, &metadataJSON, &version, &refreshJSON, &latestJSON, &attemptJSON, &board.Title, &board.Role, &board.ThreadID, &board.PrivateOwner, &privateOwner)
		if err != nil {
			return ReportWorkPage{}, err
		}
		card, err := row.toMap()
		if err != nil {
			return ReportWorkPage{}, err
		}
		var metadata, refresh, latest, attempt map[string]any
		for _, item := range []struct {
			raw string
			out *map[string]any
		}{{metadataJSON, &metadata}, {refreshJSON, &refresh}, {latestJSON.String, &latest}, {attemptJSON.String, &attempt}} {
			if item.raw != "" {
				if err := json.Unmarshal([]byte(item.raw), item.out); err != nil {
					return ReportWorkPage{}, fmt.Errorf("decode report work: %w", err)
				}
			}
		}
		page.Work = append(page.Work, projectWork(card, metadata, version, latest, attempt, refresh))
		page.Boards[workString(card["board_ref"])] = board
		page.PrivateOwners[row.CardID] = privateOwner
	}
	return page, rows.Err()
}

func (s *Store) ListWork(ctx context.Context, f WorkListFilter) (WorkPage, error) {
	page := WorkPage{Work: []map[string]any{}}
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Limit < 1 || f.Limit > 200 {
		return page, workInvalid("limit must be 1..200")
	}
	var before struct {
		UpdatedAt time.Time `json:"updated_at"`
		ID        string    `json:"id"`
	}
	if f.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err != nil || json.Unmarshal(b, &before) != nil || before.ID == "" || before.UpdatedAt.IsZero() {
			return page, ErrInvalidCursor
		}
	}
	candidates, err := s.ListReportWork(ctx, ReportWorkFilter{Limit: f.Limit, IncludeClosed: true, WorkList: &f, ProjectRef: f.ProjectRef, BeforeUpdated: strings.TrimSuffix(before.UpdatedAt.UTC().Format(time.RFC3339Nano), "Z"), BeforeID: before.ID})
	if err != nil {
		return page, err
	}
	if f.Visible != nil {
		candidates.Work, err = s.FilterCardAccess(ctx, candidates.Work, f.Visible)
		if err != nil {
			return page, err
		}
	}
	page.Work = candidates.Work
	if candidates.Truncated && len(page.Work) > 0 {
		last := page.Work[len(page.Work)-1]
		before.ID = workString(last["id"])
		before.UpdatedAt, err = workTimestamp(last["updated_at"])
		if err != nil {
			return page, err
		}
		raw, err := json.Marshal(before)
		if err != nil {
			return page, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (s *Store) ensureWorkMetadata(ctx context.Context, id, actor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,'nexus','{"source":{"authority":"nexus"}}',0,?,?) ON CONFLICT(card_id) DO NOTHING`, id, time.Now().UTC().Format(time.RFC3339Nano), actor)
	return err
}

// WorkAnnotationKeys returns the keys writable through local work annotations.
func WorkAnnotationKeys() []string {
	return []string{"labels", "roles", "project_ref", "priority", "next_actor", "next_action", "blockers", "wake_condition", "start_at", "due_at", "relations", "executions", "workspace_move", "plan", "source_refs"}
}

// ValidateWorkAnnotations is shared by proposal validation and canonical writes.
func ValidateWorkAnnotations(patch map[string]any) error {
	allowed := WorkAnnotationKeys()
	allowedSet := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = true
	}
	invalid := []string{}
	for key := range patch {
		if !allowedSet[key] {
			invalid = append(invalid, key)
		}
	}
	sort.Strings(invalid)
	if len(invalid) > 0 {
		return workInvalid("annotation keys %s are not allowed; allowed keys: %s", strings.Join(invalid, ", "), strings.Join(allowed, ", "))
	}
	if len(patch) == 0 {
		return workInvalid("annotation patch required; expected a nonempty JSON object")
	}
	return validateWorkLocal(patch)
}

func (s *Store) PatchWork(ctx context.Context, actor, identifier string, version int64, patch map[string]any) (map[string]any, error) {
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	if err := ValidateWorkAnnotations(patch); err != nil {
		return nil, err
	}
	patch = workClone(patch)
	if err := s.validateWorkReferences(ctx, patch); err != nil {
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
		return nil, &MutationOutcomeUnknown{Cause: err}
	}
	out, err := s.GetWork(ctx, id)
	if err != nil {
		return nil, &MutationOutcomeUnknown{Cause: err}
	}
	return out, nil
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

func (s *Store) submitWorkObservation(ctx context.Context, actor, identifier, token string, input map[string]any) (map[string]any, error) {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
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

func (s *Store) validateWorkReferences(ctx context.Context, m map[string]any) error {
	if ref := workString(m["project_ref"]); ref != "" {
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "topic", Ref: ref})
		if err != nil {
			return workLocalInvalid("project_ref", "must resolve to an existing workspace topic")
		}
		m["project_ref"] = resolved.CanonicalRef
	}
	if ref := workString(m["topic_ref"]); ref != "" {
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "topic", Ref: ref})
		if err != nil {
			return workLocalInvalid("topic_ref", "must resolve to an existing workspace topic")
		}
		m["topic_ref"] = resolved.CanonicalRef
	}
	if ref := workString(m["document_ref"]); ref != "" {
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "document", Ref: ref})
		if err != nil {
			return workLocalInvalid("document_ref", "must resolve to an existing workspace document")
		}
		m["document_ref"] = resolved.CanonicalRef
	}
	if _, exists := m["related_refs"]; exists {
		refs, err := optionalStringListField(m, "related_refs")
		if err != nil {
			return workLocalInvalid("related_refs", "must be an array of typed refs")
		}
		m["related_refs"] = refs
	}
	if value, ok := m["relations"]; ok {
		b, _ := json.Marshal(value)
		var relations []map[string]any
		if json.Unmarshal(b, &relations) != nil {
			return workLocalInvalid("relations", "must contain objects")
		}
		for _, relation := range relations {
			ref := workString(relation["ref"])
			kind := workString(relation["kind"])
			typ := ""
			if kind == "parent" || kind == "child" || kind == "depends_on" {
				typ = "card"
			}
			resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: typ, Ref: ref})
			if err != nil {
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidResourceRef) {
					return workLocalInvalid("relations", "relation ref must resolve inside this workspace")
				}
				return fmt.Errorf("resolve work relation: %w", err)
			}
			relation["ref"] = resolved.CanonicalRef
		}
		m["relations"] = relations
	}
	return nil
}

// WorkDecisionRevision is the proposal and dispatch fence for a projected work
// record. Native fences compose metadata version and card head revision, so
// metadata and content edits both invalidate approvals. Observation and refresh
// bookkeeping do not. Successful external reads supply the source revision.
func WorkDecisionRevision(w map[string]any) string {
	source := workMap(w["source"])
	if authority := workString(source["authority"]); authority != "" && authority != "nexus" {
		if revision, _ := source["revision"].(string); revision != "" {
			return revision
		}
	}
	version, _ := workInt(w["version"])
	head, _ := workInt(w["head_revision_number"])
	return strconv.FormatInt(version, 10) + "." + strconv.FormatInt(head, 10)
}

// LiveWorkSnapshots batches PM projection enrichment, retaining scoped SQL.
func (s *Store) LiveWorkSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	return s.decisionWorkSnapshots(ctx, refs)
}

// ReportWorkSnapshots also excludes archived boards and project topics.
func (s *Store) ReportWorkSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	return s.workSnapshots(ctx, refs, false)
}

func (s *Store) workSnapshots(ctx context.Context, refs []string, cardLifecycleOnly bool) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if len(refs) == 0 {
		return out, nil
	}
	selectors := []map[string]string{}
	for _, ref := range refs {
		value := strings.TrimSpace(ref)
		if strings.Contains(value, ":") {
			kind, suffix, err := schema.SplitTypedRef(value)
			if err != nil || kind != "card" {
				continue
			}
			value = suffix
		}
		selectors = append(selectors, map[string]string{"ref": ref, "value": value, "handle": handles.Normalize(value)})
	}
	raw, _ := json.Marshal(selectors)
	rows, err := s.db.QueryContext(ctx, `SELECT json_extract(j.value,'$.ref'),c.id FROM json_each(?) j
 LEFT JOIN resource_handle_aliases aliases ON aliases.resource_type='card' AND aliases.alias_handle=json_extract(j.value,'$.handle')
 JOIN cards c ON c.id=COALESCE((SELECT h.id FROM cards h WHERE h.handle=json_extract(j.value,'$.handle') AND h.handle IS NOT NULL AND trim(h.handle)<>'' LIMIT 1),aliases.resource_id,(SELECT canonical.id FROM cards canonical WHERE canonical.id=json_extract(j.value,'$.value'))) WHERE EXISTS(SELECT 1 FROM ref_edges placement WHERE placement.source_type='board' AND placement.target_type='card' AND placement.edge_type='board_card' AND placement.target_id=c.id)`, string(raw))
	if err != nil {
		return nil, err
	}
	byRef := map[string]string{}
	ids := []string{}
	for rows.Next() {
		var ref, id string
		if err = rows.Scan(&ref, &id); err != nil {
			rows.Close()
			return nil, err
		}
		byRef[ref] = id
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids = uniqueSortedStrings(ids)
	if len(ids) == 0 {
		return out, nil
	}
	page, err := s.ListReportWork(ctx, ReportWorkFilter{CardIDs: ids, Limit: 200, IncludeClosed: true, CardLifecycleOnly: cardLifecycleOnly})
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, w := range page.Work {
		byID[workString(w["id"])] = w
	}
	for ref, id := range byRef {
		if w, ok := byID[id]; ok {
			out[ref] = w
		}
	}
	return out, nil
}
