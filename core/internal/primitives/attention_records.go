package primitives

import (
	"agent-nexus-core/internal/scopesearch"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const attentionPageSize = 100

type AttentionPlacement struct {
	SubjectRef string `json:"subject_ref"`
	For        string `json:"for"`
	To         string `json:"to"`
	Reason     string `json:"reason"`
	Rank       int    `json:"rank"`
	RequestKey string `json:"request_key,omitempty"`
}
type AttentionFeedback struct {
	SubjectRef     string   `json:"subject_ref"`
	Signal         string   `json:"signal"`
	Note           string   `json:"note,omitempty"`
	Surface        string   `json:"surface"`
	PlacedBy       string   `json:"placed_by,omitempty"`
	SecondsVisible *float64 `json:"seconds_visible,omitempty"`
	RequestKey     string   `json:"request_key,omitempty"`
}
type AttentionItem struct {
	SubjectRef string  `json:"subject_ref"`
	Title      string  `json:"title"`
	Surface    string  `json:"surface"`
	Reason     string  `json:"reason"`
	PlacedBy   string  `json:"placed_by"`
	Rank       int     `json:"rank"`
	PlacedAt   *string `json:"placed_at"`
	Changed    bool    `json:"changed_since_last_visit"`
}
type AttentionPage struct {
	Items      []AttentionItem `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

func AttentionSurface(surface string) bool {
	return surface == "decide" || surface == "watch" || surface == "digest" || surface == "routine"
}

// Actor refs are the stable principal identity already used by asks. Explicit
// human/agent principal refs are accepted for clients that hold auth identity IDs.
func (s *Store) AttentionActor(ctx context.Context, ref string) (string, string, error) {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return "", "", workInvalid("expected actor, human or agent principal ref")
	}
	var actor, principalKind string
	var err error
	if kind == "actor" {
		err = s.db.QueryRowContext(ctx, `SELECT actor_id,COALESCE(json_extract(metadata_json,'$.principal_kind'),'agent') FROM agents WHERE actor_id=? AND revoked_at IS NULL`, id).Scan(&actor, &principalKind)
	} else if kind == "human" || kind == "agent" {
		err = s.db.QueryRowContext(ctx, `SELECT actor_id,COALESCE(json_extract(metadata_json,'$.principal_kind'),'agent') FROM agents WHERE id=? AND revoked_at IS NULL`, id).Scan(&actor, &principalKind)
		if err == nil && kind != principalKind {
			return "", "", ErrNotFound
		}
	} else {
		return "", "", workInvalid("expected actor, human or agent principal ref")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return actor, principalKind, err
}
func (s *Store) AttentionAgent(ctx context.Context) (*string, error) {
	var agent sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT attention_agent FROM attention_settings WHERE singleton=1`).Scan(&agent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !agent.Valid {
		return nil, nil
	}
	return &agent.String, nil
}
func (s *Store) SetAttentionAgent(ctx context.Context, actor string, ref *string) error {
	if ref != nil {
		id, kind, err := s.AttentionActor(ctx, *ref)
		if err != nil {
			return err
		}
		if kind != "agent" {
			return workInvalid("attention_agent must be an agent")
		}
		value := "actor:" + id
		ref = &value
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO attention_settings(singleton,attention_agent,updated_by,updated_at) VALUES(1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET attention_agent=excluded.attention_agent,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, ref, actor, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// A request key is optional for direct scripts; UI outboxes supply one. The
// canonical record, routing index, and original result commit together. Only
// feedback enters the shared event ledger; placements stay principal-scoped.
func (s *Store) attentionWrite(ctx context.Context, actor, key, eventType string, payload map[string]any, apply func(*accessTx, preparedEvent) error) (map[string]any, error) {
	if len(key) > 200 {
		return nil, workInvalid("request_key exceeds 200 bytes")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	run := func(ctx context.Context) (map[string]any, error) {
		prepared, err := prepareEventForInsert(actor, map[string]any{"type": eventType, "summary": eventType, "refs": []string{anyStringValue(payload["subject_ref"])}, "payload": payload})
		if err != nil {
			return nil, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		prepared.Body["ts"] = time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
		if eventType != "attention_placed" {
			if err = insertPreparedEvent(ctx, tx, prepared); err != nil {
				return nil, err
			}
		}
		if apply != nil {
			if err = apply(tx, prepared); err != nil {
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return prepared.Body, nil
	}
	if key == "" {
		return run(ctx)
	}
	// Use the common write-snapshot/replay path so concurrent exact retries
	// serialize before checking the receipt, and inaccessible receipts conflict.
	replay, err := s.ExecuteWithReplay(ctx, eventType, actor, key, sha256Hex(encoded), 201, run)
	return replay.Response, err
}
func (s *Store) PlaceAttention(ctx context.Context, actor string, p AttentionPlacement) (map[string]any, error) {
	if !AttentionSurface(p.To) || strings.TrimSpace(p.Reason) == "" || utf8.RuneCountInString(p.Reason) > 200 {
		return nil, workInvalid("to must be decide, watch, digest or routine; reason must contain 1 to 200 characters")
	}
	recipient, _, err := s.AttentionActor(ctx, p.For)
	if err != nil {
		return nil, err
	}
	subject, err := s.ResolveResourceRef(ctx, ResourceRefInput{Ref: p.SubjectRef})
	if err != nil {
		return nil, err
	}
	p.SubjectRef = subject.Type + ":" + subject.ID
	p.For = "actor:" + recipient
	payload := map[string]any{"subject_ref": p.SubjectRef, "for": p.For, "surface": p.To, "reason": p.Reason, "rank": p.Rank, "placed_by": "actor:" + actor}
	if err = s.CheckResourceValues(ctx, payload); err != nil {
		return nil, err
	}

	return s.attentionWrite(ctx, actor, p.RequestKey, "attention_placed", payload, func(tx *accessTx, e preparedEvent) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO attention_subjects(for_actor,subject_ref) VALUES(?,?) ON CONFLICT DO NOTHING`, recipient, p.SubjectRef); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO attention_placements(for_actor,subject_ref,placed_by,record_id,placed_at,payload_json) VALUES(?,?,?,?,?,?) ON CONFLICT(for_actor,subject_ref,placed_by) DO UPDATE SET record_id=excluded.record_id,placed_at=excluded.placed_at,payload_json=excluded.payload_json`, recipient, p.SubjectRef, "actor:"+actor, e.Body["id"], e.Body["ts"], e.PayloadJSON)
		return err
	})
}
func (s *Store) RecordAttentionFeedback(ctx context.Context, actor string, p AttentionFeedback) (map[string]any, error) {
	switch p.Signal {
	case "more_like_this", "less_like_this", "not_relevant", "should_be_decide", "should_be_watch", "already_knew", "didnt_need_me", "needed_sooner", "not_enough_context", "wrong_person", "snooze", "wanted_to_see":
	default:
		return nil, workInvalid("unknown attention feedback signal")
	}
	if !AttentionSurface(p.Surface) || utf8.RuneCountInString(p.Note) > 500 || p.SecondsVisible != nil && (*p.SecondsVisible < 0 || math.IsNaN(*p.SecondsVisible) || math.IsInf(*p.SecondsVisible, 0)) {
		return nil, workInvalid("invalid feedback surface, note or seconds_visible")
	}
	subject, err := s.ResolveResourceRef(ctx, ResourceRefInput{Ref: p.SubjectRef})
	if err != nil {
		return nil, err
	}
	payload := map[string]any{"subject_ref": subject.Type + ":" + subject.ID, "signal": p.Signal, "surface": p.Surface, "for": "actor:" + actor}
	if p.Note != "" {
		payload["note"] = p.Note
	}
	if p.PlacedBy != "" {
		payload["placed_by"] = p.PlacedBy
	}
	if p.SecondsVisible != nil {
		payload["seconds_visible"] = *p.SecondsVisible
	}
	event, err := s.attentionWrite(ctx, actor, p.RequestKey, "attention_feedback", payload, nil)
	if err != nil {
		return nil, err
	}
	return attentionFeedbackView(event), nil
}

func attentionFeedbackView(event map[string]any) map[string]any {
	payload := cloneMap(workMap(event["payload"]))
	recipient := payload["for"]
	delete(payload, "for") // The recipient is server-derived envelope metadata.
	return map[string]any{"id": event["id"], "type": "attention_feedback", "ts": event["ts"], "for": recipient, "payload": payload}
}

type attentionCursor struct {
	Phase   string `json:"p"`
	After   string `json:"a"`
	Binding string `json:"b"`
	Expires int64  `json:"e"`
	Anchor  string `json:"s,omitempty"`
}

func attentionToken(codec *scopesearch.Tokens, c attentionCursor) (*string, error) {
	c.Expires = time.Now().Add(24 * time.Hour).Unix()
	v, err := codec.SealContinuation(c)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
func parseAttentionToken(codec *scopesearch.Tokens, raw, binding string) (attentionCursor, error) {
	c := attentionCursor{Phase: "asks", Binding: binding}
	if raw == "" {
		return c, nil
	}
	if codec.OpenContinuation(raw, &c) != nil || c.Binding != binding || c.Expires <= time.Now().Unix() || (c.Phase != "asks" && c.Phase != "placements" && c.Phase != "defaults" && c.Phase != "feedback") {
		return c, ErrInvalidCursor
	}
	return c, nil
}

// Select one winner by indexed point seeks for each admitted subject. Never
// materialize every placer or scan all placements before pagination.
const attentionWinnerSQL = `COALESCE((SELECT placed_by FROM attention_placement_positions WHERE for_actor=s.for_actor AND subject_ref=s.subject_ref AND placed_by=?),(SELECT placed_by FROM attention_placement_positions WHERE for_actor=s.for_actor AND subject_ref=s.subject_ref ORDER BY placed_at DESC,record_id DESC LIMIT 1))`

func (s *Store) AttentionItems(ctx context.Context, actor, kind, surface, since, scope, cursor string, now time.Time) (AttentionPage, error) {
	out := AttentionPage{Items: []AttentionItem{}}
	binding := sha256Hex([]byte(strings.Join([]string{actor, kind, surface, since, scope}, "\x00")))
	codec, binding, err := s.workCursorCodec(ctx, WorkListFilter{Query: "attention:" + binding})
	if err != nil {
		return out, err
	}
	c, err := parseAttentionToken(codec, cursor, binding)
	if err != nil {
		return out, err
	}
	if c.Anchor == "" && since != "" {
		c.Anchor = since
		if d, e := time.ParseDuration(since); e == nil {
			c.Anchor = now.Add(-d).UTC().Format("2006-01-02T15:04:05.000000000Z")
		}
	}

	var baseline sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT visited_at FROM overview_visits WHERE principal_id=? AND scope_ref=?`, "actor:"+actor, scope).Scan(&baseline)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	agent, err := s.AttentionAgent(ctx)
	if err != nil {
		return out, err
	}
	preferred := ""
	if agent != nil {
		preferred = *agent
	}
	type candidate struct {
		ref, title, at string
		payload        map[string]any
		mandatory      bool
	}
	candidates := []candidate{}
	nextPhase := ""
	if c.Phase == "asks" {
		if kind == "human" && (surface == "all" || surface == "decide") {
			rows, err := s.db.QueryContext(ctx, `WITH admitted AS MATERIALIZED (SELECT ask_id FROM attention_ask_positions WHERE recipient=? AND state='open' AND ask_id>? ORDER BY ask_id LIMIT 101) SELECT a.ask_id,COALESCE(json_extract(e.payload_json,'$.payload.title'),''),COALESCE(e.ts,'') FROM admitted a LEFT JOIN events e ON e.id=a.ask_id AND COALESCE(e.trashed_at,'')='' AND COALESCE(e.archived_at,'')='' ORDER BY a.ask_id`, actor, c.After)
			if err != nil {
				return out, err
			}
			for rows.Next() {
				var id, title, at string
				if err = rows.Scan(&id, &title, &at); err != nil {
					rows.Close()
					return out, err
				}
				candidates = append(candidates, candidate{ref: "event:" + id, title: title, at: at, mandatory: true})
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return out, err
			}
			if len(candidates) > attentionPageSize {
				out.NextCursor, err = attentionToken(codec, attentionCursor{Phase: "asks", After: strings.TrimPrefix(candidates[attentionPageSize-1].ref, "event:"), Binding: binding, Anchor: c.Anchor})
				if err != nil {
					return out, err
				}
				candidates = candidates[:attentionPageSize]
			}
		}
		if out.NextCursor == nil {
			nextPhase = "placements"
		}
	} else if c.Phase == "placements" {
		rows, err := s.db.QueryContext(ctx, `WITH admitted AS MATERIALIZED (SELECT for_actor,subject_ref FROM attention_subjects WHERE for_actor=? AND subject_ref>? ORDER BY subject_ref LIMIT 101) SELECT s.subject_ref,COALESCE(p.placed_at,''),COALESCE(p.payload_json,'{}') FROM admitted s LEFT JOIN attention_placements p ON p.for_actor=s.for_actor AND p.subject_ref=s.subject_ref AND p.placed_by=`+attentionWinnerSQL+` ORDER BY s.subject_ref`, actor, c.After, preferred)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var r, at, raw string
			if err = rows.Scan(&r, &at, &raw); err != nil {
				rows.Close()
				return out, err
			}
			var wrapper map[string]any
			if err = json.Unmarshal([]byte(raw), &wrapper); err != nil {
				rows.Close()
				return out, err
			}
			candidates = append(candidates, candidate{ref: r, at: at, payload: workMap(wrapper["payload"])})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if len(candidates) > attentionPageSize {
			out.NextCursor, err = attentionToken(codec, attentionCursor{Phase: "placements", After: candidates[attentionPageSize-1].ref, Binding: binding, Anchor: c.Anchor})
			if err != nil {
				return out, err
			}
			candidates = candidates[:attentionPageSize]
		} else {
			nextPhase = "defaults"
		}
	} else if c.Phase == "defaults" {
		// The ordinary work cursor is authenticated and reader-bound by ListWork.
		page, err := s.ListWork(ctx, WorkListFilter{Limit: attentionPageSize, Cursor: c.After})
		if err != nil {
			return out, err
		}
		refs := []string{}
		for _, w := range page.Work {
			refs = append(refs, "card:"+anyStringValue(w["id"]))
		}
		encoded, _ := json.Marshal(refs)
		rows, err := s.db.QueryContext(ctx, `SELECT subject_ref FROM attention_subjects WHERE for_actor=? AND subject_ref IN (SELECT value FROM json_each(?))`, actor, string(encoded))
		if err != nil {
			return out, err
		}
		placed := map[string]bool{}
		for rows.Next() {
			var ref string
			if err = rows.Scan(&ref); err != nil {
				rows.Close()
				return out, err
			}
			placed[ref] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		for _, w := range page.Work {
			ref := "card:" + anyStringValue(w["id"])
			if placed[ref] || w["phase"] == "done" || w["phase"] == "cancelled" {
				continue
			}
			candidates = append(candidates, candidate{ref: ref, at: anyStringValue(w["updated_at"])})
		}
		if page.NextCursor != "" {
			out.NextCursor, err = attentionToken(codec, attentionCursor{Phase: "defaults", After: page.NextCursor, Binding: binding, Anchor: c.Anchor})
			if err != nil {
				return out, err
			}
		}
	}
	if nextPhase != "" {
		out.NextCursor, err = attentionToken(codec, attentionCursor{Phase: nextPhase, Binding: binding, Anchor: c.Anchor})
		if err != nil {
			return out, err
		}
	}
	if c.Phase == "placements" && kind == "human" {
		eventIDs := []string{}
		for _, v := range candidates {
			if strings.HasPrefix(v.ref, "event:") {
				eventIDs = append(eventIDs, strings.TrimPrefix(v.ref, "event:"))
			}
		}
		raw, _ := json.Marshal(eventIDs)
		rows, e := s.db.QueryContext(ctx, `SELECT ask_id FROM brief_asks WHERE ask_id IN (SELECT value FROM json_each(?)) AND recipient=? AND state='open'`, string(raw), actor)
		if e != nil {
			return out, e
		}
		mandatory := map[string]bool{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, e
			}
			mandatory["event:"+id] = true
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		for i, v := range candidates {
			if mandatory[v.ref] {
				candidates[i].payload = nil
			}
		}
	}
	refs := []string{}
	for _, v := range candidates {
		refs = append(refs, v.ref)
	}
	facts, err := s.readRefProjection(ctx, refs, nil, true)
	if err != nil {
		return out, err
	}
	encodedRefs, _ := json.Marshal(refs)
	eventFacts := map[string]struct{ ref, title string }{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(NULLIF(handle,''),id),COALESCE(json_extract(payload_json,'$.payload.title'),json_extract(payload_json,'$.summary'),'') FROM events WHERE id IN (SELECT substr(value,7) FROM json_each(?) WHERE value LIKE 'event:%')`, string(encodedRefs))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, handle, title string
		if err = rows.Scan(&id, &handle, &title); err != nil {
			rows.Close()
			return out, err
		}
		eventFacts["event:"+id] = struct{ ref, title string }{"event:" + handle, title}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i, ref := range refs {
		if e, ok := eventFacts[ref]; ok {
			facts[i].Resolvable = true
			facts[i].Ref = e.ref
			facts[i].Title = e.title
		}
	}
	modified := map[string]time.Time{}
	rows, err = s.db.QueryContext(ctx, `SELECT 'card:'||id,updated_at FROM cards WHERE id IN (SELECT substr(value,6) FROM json_each(?) WHERE value LIKE 'card:%') UNION ALL SELECT 'document:'||id,updated_at FROM documents WHERE id IN (SELECT substr(value,10) FROM json_each(?) WHERE value LIKE 'document:%') UNION ALL SELECT 'topic:'||id,updated_at FROM topics WHERE id IN (SELECT substr(value,7) FROM json_each(?) WHERE value LIKE 'topic:%') UNION ALL SELECT 'board:'||id,updated_at FROM boards WHERE id IN (SELECT substr(value,7) FROM json_each(?) WHERE value LIKE 'board:%')`, string(encodedRefs), string(encodedRefs), string(encodedRefs), string(encodedRefs))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var ref, at string
		if err = rows.Scan(&ref, &at); err != nil {
			rows.Close()
			return out, err
		}
		modified[ref], _ = time.Parse(time.RFC3339Nano, at)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	lower, _ := time.Parse(time.RFC3339Nano, c.Anchor)
	last, _ := time.Parse(time.RFC3339Nano, baseline.String)
	for i, v := range candidates {
		if !facts[i].Resolvable || c.Phase == "placements" && len(v.payload) == 0 {
			continue
		}
		item := AttentionItem{SubjectRef: facts[i].Ref, Title: facts[i].Title, Surface: "watch", Reason: "Active work", PlacedBy: "anx_default"}
		at, _ := time.Parse(time.RFC3339Nano, v.at)
		if v.mandatory {
			item.Title = v.title
			item.Surface = "decide"
			item.Reason = "Open ask addressed to you"
		} else if v.payload != nil {
			item.Surface = anyStringValue(v.payload["surface"])
			item.Reason = anyStringValue(v.payload["reason"])
			item.PlacedBy = anyStringValue(v.payload["placed_by"])
			item.Rank = int(numberAttention(v.payload["rank"]))
			item.PlacedAt = &v.at
		}
		if updated := modified[v.ref]; updated.After(at) {
			at = updated
		}
		item.Changed = baseline.Valid && at.After(last)
		if surface != "all" && item.Surface != surface {
			continue
		}
		// since filters optional news, never an outstanding human obligation.
		if !v.mandatory && !lower.IsZero() && !at.After(lower) {
			continue
		}
		out.Items = append(out.Items, item)
	}
	if c.Phase == "placements" {
		sort.SliceStable(out.Items, func(i, j int) bool {
			if out.Items[i].Rank != out.Items[j].Rank {
				return out.Items[i].Rank < out.Items[j].Rank
			}
			return out.Items[i].SubjectRef < out.Items[j].SubjectRef
		})
	}
	if err != nil {
		return out, err
	}
	return out, nil
}
func numberAttention(v any) float64 { n, _ := loopNumber(v); return n }

func (s *Store) AttentionFeedbackList(ctx context.Context, recipient, since, cursor string) (map[string]any, error) {
	binding := sha256Hex([]byte(recipient + "\x00" + since))
	codec, binding, err := s.workCursorCodec(ctx, WorkListFilter{Query: "attention-feedback:" + binding})
	if err != nil {
		return nil, err
	}
	c, err := parseAttentionToken(codec, cursor, binding)
	if err != nil {
		return nil, err
	}
	if c.Anchor == "" && since != "" {
		c.Anchor = since
		if d, e := time.ParseDuration(since); e == nil {
			c.Anchor = time.Now().UTC().Add(-d).Format("2006-01-02T15:04:05.000000000Z")
		}
	}
	since = c.Anchor

	if cursor != "" && c.Phase != "feedback" {
		return nil, ErrInvalidCursor
	}
	query := `SELECT id,ts FROM attention_feedback_positions WHERE type='attention_feedback'`
	args := []any{}
	if recipient != "" {
		query += ` AND for_ref=?`
		args = append(args, "actor:"+recipient)
	}
	if since != "" {
		query += ` AND ts>=?`
		args = append(args, since)
	}
	if c.After != "" {
		at, id, ok := strings.Cut(c.After, "\n")
		if !ok {
			return nil, ErrInvalidCursor
		}
		query += ` AND (ts,id)<(?,?)`
		args = append(args, at, id)
	}
	query += ` ORDER BY ts DESC,id DESC LIMIT 101`
	query = `WITH admitted AS MATERIALIZED (` + query + `) SELECT a.id,a.ts,COALESCE(e.payload_json,'{}') FROM admitted a LEFT JOIN events e ON e.id=a.id ORDER BY a.ts DESC,a.id DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	var next *string
	seen := 0
	lastID, lastAt := "", ""
	for rows.Next() {
		var id, at, raw string
		if err = rows.Scan(&id, &at, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if seen == attentionPageSize {
			next, err = attentionToken(codec, attentionCursor{Phase: "feedback", After: lastAt + "\n" + lastID, Binding: binding, Anchor: c.Anchor})
			if err != nil {
				rows.Close()
				return nil, err
			}
			break
		}
		var wrapper map[string]any
		if err = json.Unmarshal([]byte(raw), &wrapper); err != nil {
			rows.Close()
			return nil, err
		}
		seen++
		lastID, lastAt = id, at
		payload := workMap(wrapper["payload"])
		if len(payload) == 0 {
			continue
		}
		items = append(items, attentionFeedbackView(map[string]any{"id": id, "ts": at, "payload": payload}))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}
