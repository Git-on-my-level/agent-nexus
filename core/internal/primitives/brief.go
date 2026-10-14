package primitives

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// BriefSectionLimit caps every brief.v1 section.
const BriefSectionLimit = 50

const (
	briefAskCandidates  = 200
	briefDoneLimit      = 10
	briefAnsweredWindow = 7 * 24 * time.Hour
)

// BriefReader is the principal a brief is computed for.
type BriefReader struct {
	ActorID string
	Kind    string // human | agent | service
	Display string
	// Visible applies the PM-private thread rule on top of the scoped SQL
	// relations; nil admits every row the scoped SQL returned.
	Visible func(threadID, privateOwner string) bool
}

func (r BriefReader) human() bool { return r.Kind == "" || r.Kind == "human" }

func stripActor(id string) string { return strings.TrimPrefix(strings.TrimSpace(id), "actor:") }

// Brief computes brief.v1 for one reader over the materialized facts. Every
// statement is an indexed seek with a LIMIT; joins against cards and events
// go through the reader's scoped relations, so withheld rows never appear and
// counts never reveal them. Durations are computed against now.
func (s *Store) Brief(ctx context.Context, reader BriefReader, now time.Time) (map[string]any, error) {
	now = now.UTC()
	b := &briefBuild{s: s, ctx: ctx, reader: reader, now: now, actors: map[string]bool{}}
	decisions, decisionsTruncated, err := b.decisions()
	if err != nil {
		return nil, err
	}
	yourAsks, yourTruncated, err := b.yourAsks()
	if err != nil {
		return nil, err
	}
	answered, answeredTruncated, err := b.answered()
	if err != nil {
		return nil, err
	}
	initiatives, initiativesTruncated, err := b.initiatives()
	if err != nil {
		return nil, err
	}
	if err = b.resolveActors(); err != nil {
		return nil, err
	}
	b.fillPrincipals(decisions, "requested_by")
	b.fillPrincipals(yourAsks, "requested_by")
	b.fillPrincipals(answered, "answered_by")
	for _, initiative := range initiatives {
		if p, ok := initiative["promise"].(map[string]any); ok {
			b.fillPrincipals([]map[string]any{p}, "author")
		}
		if n, ok := initiative["note"].(map[string]any); ok {
			b.fillPrincipals([]map[string]any{n}, "author")
		}
	}
	return map[string]any{
		"schema":           "brief.v1",
		"generated_at":     now.Format(time.RFC3339),
		"principal":        map[string]any{"id": reader.ActorID, "display": firstNonEmptyString(reader.Display, reader.ActorID), "kind": firstNonEmptyString(reader.Kind, "human")},
		"decisions":        nonNilMaps(decisions),
		"initiatives":      nonNilMaps(initiatives),
		"your_asks":        nonNilMaps(yourAsks),
		"answered_for_you": nonNilMaps(answered),
		"truncated":        map[string]bool{"decisions": decisionsTruncated, "initiatives": initiativesTruncated, "your_asks": yourTruncated, "answered_for_you": answeredTruncated},
	}, nil
}

func nonNilMaps(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

type briefBuild struct {
	s       *Store
	ctx     context.Context
	reader  BriefReader
	now     time.Time
	actors  map[string]bool
	display map[string]map[string]any
}

type briefAskRow struct {
	BriefAsk
	thread, pmOwner, payload string
	answeredAt, responder    string
	responseText, outcome    string
}

const briefAskSelect = `SELECT ` + briefAskColumns + `,COALESCE(e.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=e.thread_id),''),e.payload_json,
	COALESCE(a.answered_at,''),a.responder,COALESCE(json_extract(r.payload_json,'$.payload.response_text'),''),COALESCE(json_extract(r.payload_json,'$.payload.outcome'),'')
	FROM brief_asks a JOIN events e ON e.id=a.ask_id LEFT JOIN events r ON r.id=a.response_event_id AND a.response_event_id<>''`

func (b *briefBuild) queryAsks(where string, args ...any) ([]briefAskRow, error) {
	rows, err := b.s.db.QueryContext(b.ctx, briefAskSelect+` WHERE COALESCE(e.trashed_at,'')='' AND `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []briefAskRow{}
	for rows.Next() {
		var a briefAskRow
		var created, decide, blocks string
		if err = rows.Scan(&a.AskID, &a.InboxItemID, &a.Kind, &a.Requester, &a.Recipient, &a.SubjectRef, &a.Title, &a.Priority, &a.State, &created, &decide, &blocks, &a.BlocksCount,
			&a.thread, &a.pmOwner, &a.payload, &a.answeredAt, &a.responder, &a.responseText, &a.outcome); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if at, e := time.Parse(time.RFC3339Nano, decide); e == nil {
			a.DecideBy = &at
		}
		var refs []any
		_ = json.Unmarshal([]byte(blocks), &refs)
		for _, r := range refs {
			if text, ok := r.(string); ok && strings.TrimSpace(text) != "" && len(a.Blocks) < 20 {
				a.Blocks = append(a.Blocks, strings.TrimSpace(text))
			}
		}
		if b.reader.Visible != nil && !b.reader.Visible(a.thread, a.pmOwner) {
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (b *briefBuild) readerIDs() (string, string) {
	id := stripActor(b.reader.ActorID)
	return id, "actor:" + id
}

// decisions are open asks addressed to the reader: recipient is the reader,
// or unaddressed asks when the reader is a human. The reader's own asks are
// your_asks, never decisions.
func (b *briefBuild) decisions() ([]map[string]any, bool, error) {
	id, prefixed := b.readerIDs()
	addressed := `(a.recipient IN (?,?)`
	if b.reader.human() {
		addressed += ` OR a.recipient=''`
	}
	addressed += `) AND a.requester NOT IN (?,?)`
	dated, err := b.queryAsks(`a.state='open' AND a.decide_by IS NOT NULL AND `+addressed+` ORDER BY a.decide_by,a.ask_id LIMIT ?`, id, prefixed, id, prefixed, briefAskCandidates)
	if err != nil {
		return nil, false, err
	}
	undated, err := b.queryAsks(`a.state='open' AND a.decide_by IS NULL AND `+addressed+` ORDER BY a.created_at,a.ask_id LIMIT ?`, id, prefixed, id, prefixed, briefAskCandidates)
	if err != nil {
		return nil, false, err
	}
	return b.renderAsks(append(dated, undated...), len(dated) == briefAskCandidates || len(undated) == briefAskCandidates)
}

func (b *briefBuild) yourAsks() ([]map[string]any, bool, error) {
	id, prefixed := b.readerIDs()
	rows, err := b.queryAsks(`a.requester IN (?,?) AND a.state='open' ORDER BY a.created_at DESC,a.ask_id LIMIT ?`, id, prefixed, briefAskCandidates)
	if err != nil {
		return nil, false, err
	}
	return b.renderAsks(rows, len(rows) == briefAskCandidates)
}

func (b *briefBuild) renderAsks(rows []briefAskRow, candidatesTruncated bool) ([]map[string]any, bool, error) {
	asks := make([]BriefAsk, len(rows))
	index := map[string]briefAskRow{}
	for i, r := range rows {
		asks[i] = r.BriefAsk
		index[r.AskID] = r
	}
	open, err := b.openBlocks(rows)
	if err != nil {
		return nil, false, err
	}
	for i := range asks {
		asks[i].Blocks = open[asks[i].AskID]
		asks[i].BlocksCount = len(asks[i].Blocks)
	}
	sortBriefAsks(asks)
	truncated := candidatesTruncated || len(asks) > BriefSectionLimit
	if len(asks) > BriefSectionLimit {
		asks = asks[:BriefSectionLimit]
	}
	subjects, err := b.cardTitles(collectSubjects(asks))
	if err != nil {
		return nil, false, err
	}
	out := make([]map[string]any, 0, len(asks))
	for _, a := range asks {
		r := index[a.AskID]
		var event map[string]any
		_ = json.Unmarshal([]byte(r.payload), &event)
		payload, _ := event["payload"].(map[string]any)
		kind := briefDecisionKind(a.Kind, payload)
		item := map[string]any{
			"ref": a.InboxItemID, "inbox_item_id": a.InboxItemID, "kind": kind, "title": a.Title,
			"requested_by": stripActor(a.Requester), "created_at": a.CreatedAt.UTC().Format(time.RFC3339),
			"age_seconds": int64(b.now.Sub(a.CreatedAt) / time.Second), "decide_by": nil, "overdue_for_seconds": nil,
			"blocks": a.blocksJSON(subjects), "blocks_count": a.BlocksCount, "proposals": briefProposals(payload),
		}
		b.actors[stripActor(a.Requester)] = true
		if body := strings.TrimSpace(anyStringValue(payload["body"])); body != "" {
			item["body"] = body
		}
		if a.Priority != "" {
			item["priority"] = a.Priority
		}
		if a.DecideBy != nil {
			item["decide_by"] = a.DecideBy.UTC().Format(time.RFC3339)
			overdue := int64(0)
			if b.now.After(*a.DecideBy) {
				overdue = int64(b.now.Sub(*a.DecideBy) / time.Second)
			}
			item["overdue_for_seconds"] = overdue
		}
		if a.SubjectRef != "" {
			subject := map[string]any{"ref": a.SubjectRef}
			if title, ok := subjects[a.SubjectRef]; ok {
				subject["title"] = title
				item["subject"] = subject
			} else if !strings.HasPrefix(a.SubjectRef, "card:") {
				item["subject"] = subject
			}
		}
		if curation, ok := payload["curation"].(map[string]any); ok {
			item["curation"] = curation
		}
		out = append(out, item)
	}
	return out, truncated, nil
}

func (a BriefAsk) blocksJSON(titles map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(a.Blocks))
	for _, ref := range a.Blocks {
		item := map[string]any{"ref": ref}
		if title, ok := titles[ref]; ok {
			item["title"] = title
		}
		out = append(out, item)
	}
	return out
}

func collectSubjects(asks []BriefAsk) []string {
	refs := []string{}
	for _, a := range asks {
		if a.SubjectRef != "" {
			refs = append(refs, a.SubjectRef)
		}
		refs = append(refs, a.Blocks...)
	}
	return refs
}

func briefDecisionKind(kind string, payload map[string]any) string {
	if _, ok := payload["curation"].(map[string]any); ok {
		return "pm_proposal"
	}
	if strings.TrimSpace(anyStringValue(payload["access_request_id"])) != "" {
		return "access"
	}
	switch kind {
	case "review", "report_review":
		return "review"
	default:
		return "ask"
	}
}

func briefProposals(payload map[string]any) []string {
	out := []string{}
	raw, _ := payload["response_proposals"].([]any)
	for _, p := range raw {
		if text := strings.TrimSpace(anyStringValue(p)); text != "" && len(out) < 6 {
			out = append(out, text)
		}
	}
	return out
}

// openBlocks drops blocks refs that reached a terminal state or that the
// reader cannot see (card refs only resolve through the scoped cards
// relation). Non-card refs pass through: they are already in the ask the
// reader can read.
func (b *briefBuild) openBlocks(rows []briefAskRow) (map[string][]string, error) {
	cardRefs := []string{}
	for _, r := range rows {
		for _, ref := range r.Blocks {
			if strings.HasPrefix(ref, "card:") {
				cardRefs = append(cardRefs, ref)
			}
		}
	}
	state, err := b.cardStates(cardRefs)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		kept := []string{}
		for _, ref := range r.Blocks {
			if strings.HasPrefix(ref, "card:") {
				phase, ok := state[ref]
				if !ok || plans.Status(phase) == "done" || phase == "cancelled" {
					continue
				}
			}
			kept = append(kept, ref)
		}
		out[r.AskID] = kept
	}
	return out, nil
}

// cardStates resolves at most 1000 card refs (id or handle) to their phase
// through the scoped cards relation; hidden, archived and trashed cards are
// absent.
func (b *briefBuild) cardStates(refs []string) (map[string]string, error) {
	out := map[string]string{}
	keys := briefCardKeys(refs)
	if len(keys) == 0 {
		return out, nil
	}
	encoded, _ := json.Marshal(keys)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT c.id,COALESCE(c.handle,''),c.column_key FROM cards c WHERE (c.id IN (SELECT value FROM json_each(?1)) OR c.handle IN (SELECT value FROM json_each(?1))) AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''`, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle, phase string
		if err = rows.Scan(&id, &handle, &phase); err != nil {
			return nil, err
		}
		out["card:"+id] = phase
		if handle != "" {
			out["card:"+handle] = phase
		}
	}
	return out, rows.Err()
}

func briefCardKeys(refs []string) []string {
	seen := map[string]bool{}
	keys := []string{}
	for _, ref := range refs {
		if key := strings.TrimPrefix(ref, "card:"); key != ref && key != "" && !seen[key] && len(keys) < 1000 {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// cardTitles resolves visible card titles for at most 1000 refs.
func (b *briefBuild) cardTitles(refs []string) (map[string]string, error) {
	out := map[string]string{}
	keys := briefCardKeys(refs)
	if len(keys) == 0 {
		return out, nil
	}
	encoded, _ := json.Marshal(keys)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT c.id,COALESCE(c.handle,''),c.title,COALESCE(c.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),'') FROM cards c WHERE (c.id IN (SELECT value FROM json_each(?1)) OR c.handle IN (SELECT value FROM json_each(?1))) AND COALESCE(c.trashed_at,'')=''`, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle, title, thread, owner string
		if err = rows.Scan(&id, &handle, &title, &thread, &owner); err != nil {
			return nil, err
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, owner) {
			continue
		}
		out["card:"+id] = title
		if handle != "" {
			out["card:"+handle] = title
		}
	}
	return out, rows.Err()
}

func (b *briefBuild) answered() ([]map[string]any, bool, error) {
	id, prefixed := b.readerIDs()
	since := b.now.Add(-briefAnsweredWindow).Format(time.RFC3339)
	rows, err := b.queryAsks(`a.requester IN (?,?) AND a.state='answered' AND a.answered_at>=? ORDER BY a.answered_at DESC,a.ask_id LIMIT ?`, id, prefixed, since, BriefSectionLimit+1)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > BriefSectionLimit
	if truncated {
		rows = rows[:BriefSectionLimit]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var event map[string]any
		_ = json.Unmarshal([]byte(r.payload), &event)
		payload, _ := event["payload"].(map[string]any)
		proposals := briefProposals(payload)
		answer := strings.TrimSpace(r.responseText)
		item := map[string]any{
			"ref": r.InboxItemID, "inbox_item_id": r.InboxItemID, "title": r.Title, "answer": answer,
			"answered_at": r.answeredAt, "answered_by": stripActor(r.responder),
			"followed_recommendation": len(proposals) > 0 && answer == proposals[0],
		}
		if at, e := time.Parse(time.RFC3339Nano, r.answeredAt); e == nil {
			item["answered_at"] = at.UTC().Format(time.RFC3339)
		}
		if r.outcome != "" {
			item["outcome"] = r.outcome
		}
		b.actors[stripActor(r.responder)] = true
		out = append(out, item)
	}
	return out, truncated, nil
}

type briefCardRow struct {
	briefInitiativeRow
	handle, title, phase, thread, owner, plan string
}

const briefInitiativeSelect = `SELECT bi.card_id,bi.open,COALESCE(bi.promise_at,''),bi.promise_what,bi.promise_source,bi.promise_source_ref,bi.promise_author,bi.promise_moves,COALESCE(bi.last_evidence_at,''),
	COALESCE(c.handle,''),c.title,c.column_key,COALESCE(c.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),''),COALESCE(p.body_json,'')
	FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id LEFT JOIN card_plans p ON p.card_id=c.id
	WHERE COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND (NOT EXISTS(SELECT 1 FROM boards WHERE role='initiatives') OR c.board_id IN (SELECT id FROM boards WHERE role='initiatives')) AND `

func (b *briefBuild) queryInitiatives(where string, args ...any) ([]briefCardRow, error) {
	rows, err := b.s.db.QueryContext(b.ctx, briefInitiativeSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []briefCardRow{}
	for rows.Next() {
		var r briefCardRow
		if err = rows.Scan(&r.cardID, &r.open, &r.promiseAt, &r.what, &r.source, &r.sourceRef, &r.author, &r.moves, &r.lastEvidence, &r.handle, &r.title, &r.phase, &r.thread, &r.owner, &r.plan); err != nil {
			return nil, err
		}
		if b.reader.Visible != nil && !b.reader.Visible(r.thread, r.owner) {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// initiatives: promised (past first, then soonest), then no_promise (oldest
// evidence first), then recently done; at most BriefSectionLimit in all.
func (b *briefBuild) initiatives() ([]map[string]any, bool, error) {
	limit := BriefSectionLimit + 1
	promised, err := b.queryInitiatives(`bi.open=1 AND bi.promise_at IS NOT NULL ORDER BY bi.promise_at,bi.card_id LIMIT ?`, limit)
	if err != nil {
		return nil, false, err
	}
	rows := promised
	if len(rows) < limit {
		none, err := b.queryInitiatives(`bi.open=1 AND bi.promise_at IS NULL ORDER BY bi.last_evidence_at,bi.card_id LIMIT ?`, limit-len(rows))
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, none...)
	}
	if len(rows) < limit {
		done, err := b.queryInitiatives(`bi.open=0 ORDER BY bi.last_evidence_at DESC,bi.card_id LIMIT ?`, min(briefDoneLimit, limit-len(rows)))
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, done...)
	}
	truncated := len(rows) > BriefSectionLimit
	if truncated {
		rows = rows[:BriefSectionLimit]
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.cardID)
	}
	counts, err := b.initiativeAskCounts(rows)
	if err != nil {
		return nil, false, err
	}
	notes, err := b.latestNotes(ids)
	if err != nil {
		return nil, false, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		ref := "card:" + firstNonEmptyString(r.handle, r.cardID)
		state := r.fold().At(b.now)
		item := map[string]any{
			"ref": ref, "title": r.title, "state": state.State, "placement": briefPlacement(r.phase),
			"promise": nil, "past_promise_for_seconds": nil, "promise_moves": state.Moves, "last_evidence_at": nil,
			"progress": briefProgress(r.plan), "blocked_count": counts[r.cardID].blocked, "waiting_on_you_count": counts[r.cardID].waiting,
			"note": nil, "possibly_moot": map[string]any{"flagged": false},
		}
		if state.Promise != nil {
			p := map[string]any{"at": state.Promise.At.UTC().Format(time.RFC3339), "what": state.Promise.What, "source": state.Promise.Source}
			if state.Promise.SourceRef != "" {
				p["source_ref"] = state.Promise.SourceRef
			}
			if state.Promise.Author != "" {
				p["author"] = stripActor(state.Promise.Author)
				b.actors[stripActor(state.Promise.Author)] = true
			}
			item["promise"] = p
		}
		if state.PastPromiseFor != nil {
			item["past_promise_for_seconds"] = int64(*state.PastPromiseFor / time.Second)
		}
		if !state.LastEvidenceAt.IsZero() {
			item["last_evidence_at"] = state.LastEvidenceAt.UTC().Format(time.RFC3339)
		}
		if n, ok := notes[r.cardID]; ok {
			item["note"] = n
			b.actors[anyStringValue(n["author"])] = true
		}
		out = append(out, item)
	}
	return out, truncated, nil
}

func briefPlacement(phase string) string {
	switch phase {
	case "backlog", "ready", "in_progress", "review", "done":
		return phase
	case "in_review":
		return "review"
	case "cancelled":
		return "done"
	case "blocked":
		return "in_progress"
	}
	if plans.Status(phase) == "done" {
		return "done"
	}
	return "backlog"
}

func briefProgress(raw string) any {
	if raw == "" {
		return nil
	}
	var p plans.Plan
	if json.Unmarshal([]byte(raw), &p) != nil || len(p.Steps) == 0 {
		return nil
	}
	done := 0
	for _, s := range p.Steps {
		if s.Status == "done" {
			done++
		}
	}
	return map[string]any{"done": done, "total": len(p.Steps)}
}

type briefAskCounts struct{ blocked, waiting int }

// initiativeAskCounts counts open asks about each card (subject) and open asks
// whose blocks name it. Subject lookups use idx_brief_asks_subject; blocks
// are read from at most 500 open asks that declare any.
func (b *briefBuild) initiativeAskCounts(rows []briefCardRow) (map[string]briefAskCounts, error) {
	out := map[string]briefAskCounts{}
	if len(rows) == 0 {
		return out, nil
	}
	byRef := map[string]string{}
	refs := []string{}
	for _, r := range rows {
		for _, ref := range []string{"card:" + r.cardID, "card:" + r.handle} {
			if ref != "card:" {
				byRef[ref] = r.cardID
				refs = append(refs, ref)
			}
		}
	}
	id, prefixed := b.readerIDs()
	encoded, _ := json.Marshal(refs)
	human := 0
	if b.reader.human() {
		human = 1
	}
	asks, err := b.queryAsks(`a.state='open' AND a.subject_ref IN (SELECT value FROM json_each(?)) LIMIT 1000`, string(encoded))
	if err != nil {
		return nil, err
	}
	for _, a := range asks {
		c := out[byRef[a.SubjectRef]]
		c.blocked++
		if (a.Recipient == id || a.Recipient == prefixed || (a.Recipient == "" && human == 1)) && stripActor(a.Requester) != id {
			c.waiting++
		}
		out[byRef[a.SubjectRef]] = c
	}
	blocking, err := b.queryAsks(`a.state='open' AND a.blocks_count>0 ORDER BY a.created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	for _, a := range blocking {
		for _, ref := range a.Blocks {
			if cardID, ok := byRef[ref]; ok && a.SubjectRef != ref {
				c := out[cardID]
				c.blocked++
				out[cardID] = c
			}
		}
	}
	return out, nil
}

// latestNotes reads the newest note per card: one indexed seek each.
func (b *briefBuild) latestNotes(cardIDs []string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for _, id := range cardIDs {
		var author, ts, excerpt string
		err := b.s.db.QueryRowContext(b.ctx, `SELECT n.author,n.ts,n.excerpt FROM brief_notes n JOIN events e ON e.id=n.event_id WHERE n.card_id=? AND COALESCE(e.trashed_at,'')='' ORDER BY n.ts DESC LIMIT 1`, id).Scan(&author, &ts, &excerpt)
		if err != nil {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, ts)
		out[id] = map[string]any{"text": excerpt, "author": stripActor(author), "at": at.UTC().Format(time.RFC3339)}
	}
	return out, nil
}

func (b *briefBuild) resolveActors() error {
	b.display = map[string]map[string]any{}
	ids := []string{}
	for id := range b.actors {
		if id != "" && len(ids) < 500 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(ids)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT a.id,a.display_name,EXISTS(SELECT 1 FROM agents g WHERE g.actor_id=a.id) FROM actors a WHERE a.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		var agent bool
		if err = rows.Scan(&id, &name, &agent); err != nil {
			return err
		}
		kind := "human"
		if agent {
			kind = "agent"
		}
		b.display[id] = map[string]any{"id": id, "display": firstNonEmptyString(name, id), "kind": kind}
	}
	return rows.Err()
}

func (b *briefBuild) fillPrincipals(items []map[string]any, field string) {
	for _, item := range items {
		id, ok := item[field].(string)
		if !ok {
			continue
		}
		if p, ok := b.display[id]; ok {
			item[field] = p
		} else {
			item[field] = map[string]any{"id": id, "display": firstNonEmptyString(id, "unknown")}
		}
	}
}
