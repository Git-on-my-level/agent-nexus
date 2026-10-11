package primitives

import (
	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/schema"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PMCardSnapshots hydrates only explicitly selected refs. The full projection
// batches card bodies, metadata, observations, plans and their evidence.
type pmBoundedReadKey struct{}

func (s *Store) BeginPMCardRead(ctx context.Context) (context.Context, func(), error) {
	return s.beginSummaryRead(context.WithValue(ctx, pmBoundedReadKey{}, true))
}

func (s *Store) PMCardSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	if len(refs) > 15 {
		return nil, ErrInvalidWorkRequest
	}
	ctx, closeRead, err := s.BeginPMCardRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()
	// Alias lookup admits at most eight indexed routing keys, no alias reason
	// or other metadata. Canonical card hydration below remains requester-scoped.
	selectors := []map[string]string{}
	for _, ref := range refs {
		kind, value, e := schema.SplitTypedRef(ref)
		if e == nil && kind == "card" {
			selectors = append(selectors, map[string]string{"ref": ref, "value": value, "handle": handles.Normalize(value)})
		}
	}
	selectorJSON, _ := json.Marshal(selectors)
	routes, err := s.db.QueryContext(ctx, `SELECT json_extract(j.value,'$.ref'),
 COALESCE((SELECT h.id FROM cards h WHERE h.handle=json_extract(j.value,'$.handle') AND h.handle IS NOT NULL AND trim(h.handle)<>'' LIMIT 1),
 (SELECT a.resource_id FROM pm_card_alias_positions a WHERE a.resource_type='card' AND a.alias_handle=json_extract(j.value,'$.handle')),
 (SELECT c.id FROM cards c WHERE c.id=json_extract(j.value,'$.value')))
 FROM json_each(?) j`, string(selectorJSON))
	if err != nil {
		return nil, err
	}
	resolved := map[string]string{}
	canonical := []string{}
	for routes.Next() {
		var ref string
		var id sql.NullString
		if err = routes.Scan(&ref, &id); err != nil {
			routes.Close()
			return nil, err
		}
		if id.Valid {
			resolved[ref] = "card:" + id.String
			canonical = append(canonical, "card:"+id.String)
		}
	}
	err = routes.Err()
	routes.Close()
	if err != nil {
		return nil, err
	}
	byCanonical, err := s.SummaryCardSnapshots(ctx, canonical)
	cards := map[string]map[string]any{}
	for ref, canonicalRef := range resolved {
		if c := byCanonical[canonicalRef]; c != nil {
			copy := map[string]any{}
			for k, v := range c {
				copy[k] = v
			}
			cards[ref] = copy
		}
	}
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, c := range cards {
		if c["trashed_at"] != nil || c["archived_at"] != nil {
			continue
		}
		ids = append(ids, workString(c["id"]))
	}
	if len(ids) == 0 {
		return map[string]map[string]any{}, nil
	}
	uniqueCards := []map[string]any{}
	seen := map[string]bool{}
	for _, card := range cards {
		id := workString(card["id"])
		if !seen[id] && card["trashed_at"] == nil && card["archived_at"] == nil {
			seen[id] = true
			uniqueCards = append(uniqueCards, card)
		}
	}
	if err = s.EnrichCardPlans(WithLegacyCardPlans(ctx), uniqueCards, nil, time.Now().UTC(), plans.DefaultStalledAfter); err != nil {
		return nil, err
	}
	// Reuse enriched canonical content without sharing the mutable pin label.
	enriched := map[string]map[string]any{}
	for _, card := range uniqueCards {
		enriched[workString(card["id"])] = card
	}
	for ref, card := range cards {
		if c := enriched[workString(card["id"])]; c != nil {
			copy := map[string]any{}
			for k, v := range c {
				copy[k] = v
			}
			cards[ref] = copy
		}
	}
	raw, _ := json.Marshal(uniqueSortedStrings(ids))
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,COALESCE(m.metadata_json,'{"source":{"authority":"nexus"}}'),COALESCE(m.version,0),COALESCE(m.refresh_json,'{}'),COALESCE(o.body_json,'null'),COALESCE(a.body_json,'null') FROM json_each(?) j JOIN cards c ON c.id=j.value LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id LEFT JOIN work_observations a ON a.id=m.latest_attempt_id`, string(raw))
	if err != nil {
		return nil, err
	}
	type metadata struct {
		version                        int64
		body, refresh, latest, attempt map[string]any
	}
	byID := map[string]metadata{}
	for rows.Next() {
		var id, body, refresh, latest, attempt string
		var m metadata
		if err = rows.Scan(&id, &body, &m.version, &refresh, &latest, &attempt); err != nil {
			rows.Close()
			return nil, err
		}
		for _, v := range []struct {
			raw    string
			target *map[string]any
		}{{body, &m.body}, {refresh, &m.refresh}, {latest, &m.latest}, {attempt, &m.attempt}} {
			if err = json.Unmarshal([]byte(v.raw), v.target); err != nil {
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
	out := map[string]map[string]any{}
	for ref, card := range cards {
		if m, ok := byID[workString(card["id"])]; ok {
			out[ref] = projectWork(card, m.body, m.version, m.latest, m.attempt, m.refresh)
		}
	}
	return out, nil
}

// PMCardActivity selects fixed candidate windows before decoding/authorization.
// Dense private or archived prefixes stay partial rather than scanning history.
// One SQL call per family; indexed per-card branches avoid N+1 round trips.
func (s *Store) PMCardActivity(ctx context.Context, cards []map[string]any, asks bool) (map[string][]map[string]any, error) {
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()
	out := map[string][]map[string]any{}
	if len(cards) == 0 {
		return out, nil
	}
	if len(cards) > 15 {
		return nil, ErrInvalidWorkRequest
	}
	branches := []string{}
	args := []any{}
	for _, c := range cards {
		id := workString(c["id"])
		thread := workString(c["thread_id"])
		if asks {
			branches = append(branches, `SELECT * FROM (SELECT ? AS card_id,e.id,COALESCE(e.handle,''),e.type,e.ts,e.actor_id,e.thread_id,e.refs_json,e.payload_json FROM (SELECT ask_id FROM pm_card_ask_positions WHERE card_id=? AND open=1 ORDER BY ask_id LIMIT 6) a JOIN events e ON e.id=a.ask_id WHERE e.trashed_at IS NULL AND e.archived_at IS NULL)`)
			args = append(args, id, id)
		} else {
			branches = append(branches, `SELECT * FROM (SELECT ? AS card_id,e.id,COALESCE(e.handle,''),e.type,e.ts,e.actor_id,e.thread_id,e.refs_json,e.payload_json FROM (SELECT id FROM pm_card_event_positions WHERE thread_id=? ORDER BY ts DESC,id DESC LIMIT 6) candidates JOIN events e ON e.id=candidates.id WHERE e.trashed_at IS NULL AND e.archived_at IS NULL)`)
			args = append(args, id, thread)
		}
	}
	rows, err := s.db.QueryContext(ctx, strings.Join(branches, " UNION ALL "), args...)
	if err != nil {
		return nil, fmt.Errorf("PM card activity: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var card, id, handle, kind, ts, actor, refs, payload string
		var thread sql.NullString
		if err = rows.Scan(&card, &id, &handle, &kind, &ts, &actor, &thread, &refs, &payload); err != nil {
			return nil, err
		}
		var wrapper map[string]any
		if err = json.Unmarshal([]byte(payload), &wrapper); err != nil {
			return nil, err
		}
		label := handle
		if label == "" {
			label = id
		}
		event := map[string]any{"ref": "event:" + label, "type": kind, "at": ts, "actor": actor}
		if v, ok := wrapper["summary"]; ok {
			event["summary"] = v
		}
		content, _ := wrapper["payload"].(map[string]any)
		for _, key := range []string{"text", "body", "title", "question", "options", "recommended_answer", "response_text"} {
			if v, ok := content[key]; ok {
				event[key] = v
			}
		}
		out[card] = append(out[card], event)
	}
	return out, rows.Err()
}

// AppendApprovedCardNote fences the revision and inserts the deterministic
// action event in one transaction. This is specific to approved PM notes, not a
// general write-burst create/idempotency facility.
var ErrApprovedCardNoteStale = errors.New("approved card note revision changed")

func (s *Store) AppendApprovedCardNote(ctx context.Context, actor, ref, revision, actionID, note string) error {
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(note) + 1024)}, blobLedgerWritePlan{}); err != nil {
		return err
	}
	w, err := s.GetWork(ctx, ref)
	if err != nil {
		return err
	}
	id := workString(w["id"])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE cards SET version=version WHERE id=?`, id); err != nil {
		return err
	}
	var head, version int64
	var authority, thread string
	err = tx.QueryRowContext(ctx, `SELECT c.head_revision_number,COALESCE(m.version,0),COALESCE(m.authority,'nexus'),c.thread_id FROM cards c JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id WHERE c.id=? AND c.trashed_at IS NULL AND c.archived_at IS NULL AND b.trashed_at IS NULL AND b.archived_at IS NULL`, id).Scan(&head, &version, &authority, &thread)
	if err != nil {
		return err
	}
	if authority != "nexus" || fmt.Sprintf("%d.%d", version, head) != revision {
		return ErrApprovedCardNoteStale
	}
	eventID := "pm_note_" + actionID
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE id=?`, eventID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		prepared, e := prepareEventForInsert(actor, map[string]any{"id": eventID, "type": "message_posted", "thread_id": thread, "refs": []string{ref}, "payload": map[string]any{"text": note}})
		if e != nil {
			return e
		}
		if err = insertPreparedEvent(ctx, tx, prepared); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return &MutationOutcomeUnknown{Cause: err}
	}
	return nil
}
