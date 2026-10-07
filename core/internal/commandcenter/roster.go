package commandcenter

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SQLIdentities reads the canonical host-agent relation. Standalone principals
// remain visible while they await adoption, but cannot report runs or presence.
type SQLIdentities struct{ DB *sql.DB }

func (s SQLIdentities) ListAgents(ctx context.Context) ([]Identity, error) {
	return s.listAgents(ctx, 0)
}
func (s SQLIdentities) ListAgentsBounded(ctx context.Context, limit int) ([]Identity, error) {
	return s.listAgents(ctx, limit)
}
func (s SQLIdentities) listAgents(ctx context.Context, limit int) ([]Identity, error) {
	suffix := ""
	args := []any{}
	if limit > 0 {
		suffix = " LIMIT ?"
		args = append(args, limit)
	}
	rows, e := resourceaccess.NewDB(s.DB).QueryContext(ctx, `SELECT a.id,a.username,a.actor_id,a.metadata_json,
		COALESCE(x.display_name,a.username),COALESCE(a.revoked_at,''),
		COALESCE(ha.name,''),COALESCE(ha.identity_kind,''),
		COALESCE(h.id,''),COALESCE(h.slug,''),
		COALESCE(h.bridge_checked_in_at,''),COALESCE(h.bridge_expires_at,''),
		COALESCE(h.revoked_at,''),CASE WHEN hx.name IS NULL THEN 0 ELSE 1 END
		FROM agents a
		LEFT JOIN actors x ON x.id=a.actor_id
		LEFT JOIN host_agents ha ON ha.agent_id=a.id
		LEFT JOIN hosts h ON h.id=ha.host_id
		LEFT JOIN host_exclusions hx ON hx.host_id=ha.host_id AND hx.name=ha.name
		WHERE COALESCE(json_extract(a.metadata_json,'$.principal_kind'),'agent')='agent'
		ORDER BY a.username`+suffix, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var v Identity
		var meta, hostRevoked string
		var excluded int
		if e = rows.Scan(&v.AgentID, &v.Handle, &v.ActorID, &meta, &v.DisplayName, &v.RevokedAt,
			&v.Name, &v.Kind, &v.HostID, &v.HostSlug, &v.BridgeCheckedInAt, &v.BridgeExpiresAt,
			&hostRevoked, &excluded); e != nil {
			return nil, e
		}
		if v.HostID != "" {
			v.DisplayName = v.Name + " on " + v.HostSlug
			v.Adapter = defaultAdapter(v.Name)
			if hostRevoked != "" || v.RevokedAt != "" || excluded != 0 {
				v.BridgeCheckedInAt, v.BridgeExpiresAt = "", ""
			}
		} else {
			var m map[string]any
			_ = json.Unmarshal([]byte(meta), &m)
			if registration, ok := m["wake_registration"].(map[string]any); ok {
				v.BridgeCheckedInAt = str(registration["bridge_checked_in_at"])
				v.BridgeExpiresAt = str(registration["bridge_expires_at"])
			}
			v.Kind = "standalone"
			v.Name = v.Handle
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s SQLIdentities) ResolveAgent(ctx context.Context, id string) (Identity, error) {
	all, e := s.ListAgents(ctx)
	if e != nil {
		return Identity{}, e
	}
	id = strings.TrimPrefix(id, "actor:")
	for _, v := range all {
		if v.AgentID == id || v.ActorID == id || v.Handle == id {
			return v, nil
		}
	}
	return Identity{}, ErrNotFound
}
func str(v any) string { s, _ := v.(string); return s }

func defaultAdapter(name string) string {
	switch name {
	case "claude", "codex", "cursor", "omp", "generic":
		return name
	default:
		return "generic"
	}
}

type ActiveRun struct {
	RunID           string  `json:"run_id"`
	Adapter         string  `json:"adapter"`
	Model           *string `json:"model"`
	DurationSeconds int64   `json:"duration_seconds"`
}
type OpenAsk struct {
	Total            int     `json:"-"`
	ID               string  `json:"id"`
	InboxItemID      *string `json:"inbox_item_id"`
	Title            string  `json:"title"`
	Severity         *string `json:"severity"`
	CreatedAt        string  `json:"created_at"`
	Kind             string  `json:"kind,omitempty"`
	SubjectRef       *string `json:"subject_ref,omitempty"`
	SubjectTitle     string  `json:"subject_title,omitempty"`
	RequesterActorID string  `json:"requester_actor_id,omitempty"`
	RequesterAgentID *string `json:"requester_agent_id,omitempty"`
}
type Summary struct {
	ID               string     `json:"id"`
	Ref              string     `json:"ref"`
	ActorID          string     `json:"actor_id"`
	HostID           *string    `json:"host_id"`
	HostSlug         *string    `json:"host_slug"`
	Name             string     `json:"name"`
	Handle           string     `json:"handle"`
	DisplayName      string     `json:"display_name"`
	IdentityKind     string     `json:"identity_kind"`
	State            string     `json:"state"`
	BridgeOnline     bool       `json:"bridge_online"`
	CurrentCardRef   *string    `json:"current_card_ref"`
	CurrentCardTitle *string    `json:"current_card_title"`
	LastProgressNote *string    `json:"last_progress_note"`
	LastProgressAt   *string    `json:"last_progress_at"`
	ActiveRun        *ActiveRun `json:"active_run"`
	OpenAsksCount    int        `json:"open_asks_count"`
	WaitingAsk       *OpenAsk   `json:"waiting_ask"`
	LastSignalAt     *string    `json:"last_signal_at"`
	RevokedAt        *string    `json:"revoked_at"`
}
type Detail struct {
	Agent       Summary          `json:"agent"`
	RecentCards []map[string]any `json:"recent_cards"`
	RecentRuns  []Run            `json:"recent_runs"`
	OpenAsks    []OpenAsk        `json:"open_asks"`
	RecentNotes []map[string]any `json:"recent_notes"`
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func later(a, b string) string {
	if a == "" {
		return b
	}
	left, leftErr := parseTime(a)
	right, rightErr := parseTime(b)
	if leftErr == nil && rightErr == nil && right.After(left) {
		return b
	}
	return a
}
func (s *Store) Roster(ctx context.Context, now time.Time) ([]Summary, error) {
	out, _, err := s.roster(ctx, now, 0)
	return out, err
}
func (s *Store) OverviewRoster(ctx context.Context, now time.Time) ([]Summary, bool, error) {
	return s.roster(ctx, now, 100)
}
func (s *Store) roster(ctx context.Context, now time.Time, limit int) ([]Summary, bool, error) {
	var identities []Identity
	var e error
	if bounded, ok := s.Identities.(interface {
		ListAgentsBounded(context.Context, int) ([]Identity, error)
	}); ok && limit > 0 {
		identities, e = bounded.ListAgentsBounded(ctx, limit+1)
	} else {
		identities, e = s.Identities.ListAgents(ctx)
	}
	partial := false
	if limit > 0 && len(identities) > limit {
		partial = true
		identities = identities[:limit]
	}
	ids, actors := []string{}, []string{}
	for _, v := range identities {
		ids = append(ids, v.AgentID)
		actors = append(actors, v.ActorID)
	}
	idJSON, _ := json.Marshal(ids)
	actorJSON, _ := json.Marshal(actors)
	if e != nil {
		return nil, false, e
	}
	out := make([]Summary, len(identities))
	index := map[string]int{}
	for i, v := range identities {
		out[i] = Summary{ID: v.AgentID, Ref: "actor:" + v.ActorID, ActorID: v.ActorID, HostID: stringPtr(v.HostID), HostSlug: stringPtr(v.HostSlug), Name: v.Name, Handle: v.Handle, DisplayName: v.DisplayName, IdentityKind: v.Kind, State: "stale", RevokedAt: stringPtr(v.RevokedAt), BridgeOnline: bridgeOnline(v.BridgeExpiresAt, now)}
		out[i].LastSignalAt = stringPtr(v.BridgeCheckedInAt)
		index[v.AgentID] = i
	}
	// Each source is scanned once. No per-agent event query is performed.
	presenceQuery := `SELECT agent_id,current_card_ref,note,observed_at FROM agent_presence`
	presenceArgs := []any{}
	if limit > 0 {
		presenceQuery += ` WHERE agent_id IN (SELECT value FROM json_each(?))`
		presenceArgs = append(presenceArgs, string(idJSON))
	}
	rows, e := resourceaccess.NewDB(s.DB).QueryContext(ctx, presenceQuery, presenceArgs...)
	if e != nil {
		return nil, false, e
	}
	for rows.Next() {
		var id, at string
		var card, note sql.NullString
		if e = rows.Scan(&id, &card, &note, &at); e != nil {
			rows.Close()
			return nil, false, e
		}
		if i, ok := index[id]; ok {
			v := &out[i]
			v.CurrentCardRef = ptr(card)
			v.LastProgressNote = ptr(note)
			v.LastProgressAt = stringPtr(at)
			v.LastSignalAt = stringPtr(later(value(v.LastSignalAt), at))
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, false, e
	}
	runQuery := "SELECT " + runColumns + " FROM runs"
	runArgs := []any{}
	if limit > 0 {
		runQuery += ` WHERE id IN (
 SELECT (SELECT id FROM runs r WHERE r.agent_id=j.value ORDER BY anx_timestamp_key(r.last_observed_at) DESC,r.id DESC LIMIT 1) FROM json_each(?) j
 UNION SELECT (SELECT id FROM runs r WHERE r.agent_id=j.value AND r.state NOT IN ('completed','failed','cancelled') AND r.liveness='alive' AND anx_timestamp_key(r.last_observed_at) BETWEEN anx_timestamp_key(?) AND anx_timestamp_key(?) ORDER BY anx_timestamp_key(r.last_observed_at) DESC,r.id DESC LIMIT 1) FROM json_each(?) j)`
		runArgs = append(runArgs, string(idJSON), now.Add(-ActiveRunFreshness).Format(time.RFC3339Nano), now.Add(RunObservationFutureSkew).Format(time.RFC3339Nano), string(idJSON))
	}
	runQuery += ` ORDER BY anx_timestamp_key(last_observed_at) DESC`
	runRows, e := resourceaccess.NewDB(s.DB).QueryContext(ctx, runQuery, runArgs...)
	if e != nil {
		return nil, false, e
	}
	for runRows.Next() {
		r, scanErr := readRun(runRows)
		if scanErr != nil {
			runRows.Close()
			return nil, false, scanErr
		}
		if i, ok := index[r.AgentID]; ok {
			v := &out[i]
			v.LastSignalAt = stringPtr(later(value(v.LastSignalAt), r.LastObservedAt))
			if activeAt(r, now) && v.ActiveRun == nil {
				start := r.LastObservedAt
				if r.StartedAt != nil {
					start = *r.StartedAt
				}
				t, _ := parseTime(start)
				secs := int64(now.Sub(t).Seconds())
				if secs < 0 {
					secs = 0
				}
				v.ActiveRun = &ActiveRun{r.ID, r.Adapter, r.Model, secs}
				if v.CurrentCardRef == nil {
					v.CurrentCardRef = r.CardRef
				}
			}
		}
	}
	e = runRows.Err()
	runRows.Close()
	if e != nil {
		return nil, false, e
	}
	eventQuery := `SELECT actor_id,ts FROM (SELECT actor_id,ts,ROW_NUMBER() OVER (PARTITION BY actor_id ORDER BY anx_timestamp_key(ts) DESC,id DESC) AS row_number FROM events) WHERE row_number=1`
	eventArgs := []any{}
	if limit > 0 {
		eventQuery = `SELECT actor_id,ts FROM events WHERE id IN (SELECT (SELECT id FROM events e WHERE e.actor_id=j.value ORDER BY anx_timestamp_key(e.ts) DESC,e.id DESC LIMIT 1) FROM json_each(?) j)`
		eventArgs = append(eventArgs, string(actorJSON))
	}
	rows, e = resourceaccess.NewDB(s.DB).QueryContext(ctx, eventQuery, eventArgs...)
	if e != nil {
		return nil, false, e
	}
	actorIndex := map[string]int{}
	for i, v := range out {
		actorIndex[v.ActorID] = i
	}
	for rows.Next() {
		var id, at string
		if e = rows.Scan(&id, &at); e != nil {
			rows.Close()
			return nil, false, e
		}
		if i, ok := actorIndex[id]; ok {
			out[i].LastSignalAt = stringPtr(later(value(out[i].LastSignalAt), at))
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, false, e
	}
	identityJSON, _ := json.Marshal(identities)
	asks, e := s.openAsksLimit(ctx, limit, string(idJSON), string(identityJSON))
	if e != nil {
		return nil, false, e
	}
	for _, ask := range asks {
		i, ok := index[value(ask.RequesterAgentID)]
		if !ok {
			i, ok = actorIndex[ask.RequesterActorID]
		}
		if ok {
			out[i].OpenAsksCount += ask.Total
			if out[i].WaitingAsk == nil {
				copy := ask
				out[i].WaitingAsk = &copy
			}
		}
	}
	cardQuery := `SELECT id,handle,title FROM cards`
	cardArgs := []any{}
	if limit > 0 {
		refs := []string{}
		for _, v := range out {
			if v.CurrentCardRef != nil {
				refs = append(refs, strings.TrimPrefix(*v.CurrentCardRef, "card:"))
			}
		}
		raw, _ := json.Marshal(refs)
		cardQuery += ` WHERE id IN (SELECT value FROM json_each(?)) OR (handle IN (SELECT value FROM json_each(?)) AND handle IS NOT NULL AND trim(handle)<>'')`
		cardArgs = append(cardArgs, string(raw), string(raw))
	}
	cardRows, e := resourceaccess.NewDB(s.DB).QueryContext(ctx, cardQuery, cardArgs...)
	if e != nil {
		return nil, false, e
	}
	cardTitles := map[string]string{}
	for cardRows.Next() {
		var id, handle, title string
		if e = cardRows.Scan(&id, &handle, &title); e != nil {
			cardRows.Close()
			return nil, false, e
		}
		cardTitles[id] = title
		cardTitles[handle] = title
	}
	e = cardRows.Err()
	cardRows.Close()
	if e != nil {
		return nil, false, e
	}
	for i := range out {
		v := &out[i]
		if v.CurrentCardRef != nil {
			v.CurrentCardTitle = stringPtr(cardTitles[strings.TrimPrefix(*v.CurrentCardRef, "card:")])
		}
		if v.OpenAsksCount > 0 {
			v.State = "waiting_on_human"
			continue
		}
		if v.ActiveRun != nil || fresh(value(v.LastProgressAt), now, PresenceFreshness) {
			v.State = "working"
			continue
		}
		if fresh(value(v.LastSignalAt), now, SignalStaleness) {
			v.State = "idle"
		}
	}
	return out, partial, nil
}
func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func fresh(raw string, now time.Time, d time.Duration) bool {
	if raw == "" {
		return false
	}
	t, e := parseTime(raw)
	return e == nil && !t.After(now) && now.Sub(t) <= d
}
func bridgeOnline(expires string, now time.Time) bool {
	t, err := parseTime(expires)
	return err == nil && t.After(now) && t.Sub(now) <= BridgeFreshness
}
func (s *Store) openAsks(ctx context.Context) ([]OpenAsk, error) {
	return s.openAsksLimit(ctx, 0, "", "")
}
func (s *Store) openAsksLimit(ctx context.Context, limit int, ids, identities string) ([]OpenAsk, error) {
	query := `SELECT req.id,req.ts,COALESCE(json_extract(req.payload_json,'$.summary'),''),req.payload_json,
		COALESCE((SELECT di.id FROM derived_inbox_items di WHERE di.source_event_id=req.id AND di.category IN ('ask','review','escalate') ORDER BY di.id LIMIT 1),''),
		COALESCE((SELECT di.data_json FROM derived_inbox_items di WHERE di.source_event_id=req.id AND di.category IN ('ask','review','escalate') ORDER BY di.id LIMIT 1),'')
		FROM events req WHERE req.type='human_attention_requested' AND req.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM events resp WHERE resp.type='human_attention_responded' AND resp.trashed_at IS NULL AND (json_extract(resp.payload_json,'$.payload.request_event_id')=req.id OR json_extract(resp.payload_json,'$.payload.request_event_ref')='event:'||req.id OR json_extract(resp.payload_json,'$.payload.request_event_ref')='event:'||req.handle)) AND NOT EXISTS (SELECT 1 FROM events withdrawn WHERE withdrawn.type='human_attention_withdrawn' AND withdrawn.trashed_at IS NULL AND (json_extract(withdrawn.payload_json,'$.payload.request_event_id')=req.id OR json_extract(withdrawn.payload_json,'$.payload.request_event_ref')='event:'||req.id OR json_extract(withdrawn.payload_json,'$.payload.request_event_ref')='event:'||req.handle))
		 ORDER BY anx_timestamp_key(req.ts),req.id`
	args := []any{}
	if limit > 0 {
		query = `WITH candidates AS (SELECT req.id,req.ts,
 CASE WHEN json_extract(req.payload_json,'$.payload.requester_agent_id') IN (SELECT value FROM json_each(?)) THEN json_extract(req.payload_json,'$.payload.requester_agent_id') WHEN EXISTS(SELECT 1 FROM agents known WHERE known.id=json_extract(req.payload_json,'$.payload.requester_agent_id')) THEN NULL ELSE (SELECT json_extract(j.value,'$.AgentID') FROM json_each(?) j WHERE json_extract(j.value,'$.ActorID')=json_extract(req.payload_json,'$.payload.requester_actor_id') ORDER BY j.key DESC LIMIT 1) END AS target
 FROM events req WHERE req.type='human_attention_requested' AND req.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM events resp WHERE resp.type='human_attention_responded' AND resp.trashed_at IS NULL AND (json_extract(resp.payload_json,'$.payload.request_event_id')=req.id OR json_extract(resp.payload_json,'$.payload.request_event_ref')='event:'||req.id OR json_extract(resp.payload_json,'$.payload.request_event_ref')='event:'||req.handle)) AND NOT EXISTS (SELECT 1 FROM events withdrawn WHERE withdrawn.type='human_attention_withdrawn' AND withdrawn.trashed_at IS NULL AND (json_extract(withdrawn.payload_json,'$.payload.request_event_id')=req.id OR json_extract(withdrawn.payload_json,'$.payload.request_event_ref')='event:'||req.id OR json_extract(withdrawn.payload_json,'$.payload.request_event_ref')='event:'||req.handle))), ranked AS (SELECT id,count(*) OVER(PARTITION BY target) AS total,row_number() OVER(PARTITION BY target ORDER BY anx_timestamp_key(ts),id) AS n FROM candidates WHERE target IS NOT NULL)
 SELECT req.id,req.ts,COALESCE(json_extract(req.payload_json,'$.summary'),''),req.payload_json,
		COALESCE((SELECT di.id FROM derived_inbox_items di WHERE di.source_event_id=req.id AND di.category IN ('ask','review','escalate') ORDER BY di.id LIMIT 1),''),
		COALESCE((SELECT di.data_json FROM derived_inbox_items di WHERE di.source_event_id=req.id AND di.category IN ('ask','review','escalate') ORDER BY di.id LIMIT 1),''),ranked.total FROM ranked JOIN events req ON req.id=ranked.id WHERE ranked.n=1 ORDER BY anx_timestamp_key(req.ts),req.id`
		args = append(args, ids, identities)
	}
	rows, e := resourceaccess.NewDB(s.DB).QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []OpenAsk{}
	for rows.Next() {
		var id, at, summary, raw, inboxID, inboxRaw string
		total := 1
		scan := []any{&id, &at, &summary, &raw, &inboxID, &inboxRaw}
		if limit > 0 {
			scan = append(scan, &total)
		}
		if e = rows.Scan(scan...); e != nil {
			return nil, e
		}
		var wrapped map[string]any
		if json.Unmarshal([]byte(raw), &wrapped) != nil {
			continue
		}
		p, ok := wrapped["payload"].(map[string]any)
		if !ok {
			continue
		}
		var inbox map[string]any
		if inboxRaw != "" {
			_ = json.Unmarshal([]byte(inboxRaw), &inbox)
		}
		ask := OpenAsk{Total: total, ID: id, InboxItemID: stringPtr(inboxID), CreatedAt: at,
			Kind: str(p["kind"]), SubjectRef: stringPtr(str(p["subject_ref"])),
			SubjectTitle: str(p["subject_title"]), RequesterActorID: str(p["requester_actor_id"]),
			RequesterAgentID: stringPtr(str(p["requester_agent_id"]))}
		ask.Title = str(p["title"])
		if ask.Title == "" {
			ask.Title = str(inbox["title"])
		}
		if ask.Title == "" {
			ask.Title = ask.SubjectTitle
		}
		if ask.Title == "" {
			ask.Title = summary
		}
		if ask.Title == "" {
			ask.Title = "Open ask"
		}
		ask.Severity = stringPtr(str(p["severity"]))
		if ask.Severity == nil {
			ask.Severity = stringPtr(str(inbox["severity"]))
		}
		out = append(out, ask)
	}
	return out, rows.Err()
}
func (s *Store) AgentDetail(ctx context.Context, id string, now time.Time) (Detail, error) {
	all, e := s.Roster(ctx, now)
	if e != nil {
		return Detail{}, e
	}
	id = strings.TrimPrefix(id, "actor:")
	var d Detail
	found := false
	for _, v := range all {
		if v.ID == id || v.ActorID == id || v.Handle == id {
			d.Agent = v
			found = true
			break
		}
	}
	if !found {
		return Detail{}, ErrNotFound
	}
	d.RecentCards = []map[string]any{}
	d.RecentRuns = []Run{}
	d.OpenAsks = []OpenAsk{}
	d.RecentNotes = []map[string]any{}
	runs, _, e := s.ListRuns(ctx, Filter{AgentID: d.Agent.ID, Limit: 50})
	if e != nil {
		return d, e
	}
	d.RecentRuns = runs
	asks, e := s.openAsks(ctx)
	if e != nil {
		return d, e
	}
	for _, v := range asks {
		if value(v.RequesterAgentID) == d.Agent.ID || v.RequesterActorID == d.Agent.ActorID {
			d.OpenAsks = append(d.OpenAsks, v)
		}
	}
	rows, e := resourceaccess.NewDB(s.DB).QueryContext(ctx, `SELECT text,observed_at,card_ref FROM agent_progress_notes WHERE agent_id=? ORDER BY observed_at DESC LIMIT 50`, d.Agent.ID)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var text, at string
		var card sql.NullString
		if e = rows.Scan(&text, &at, &card); e != nil {
			rows.Close()
			return d, e
		}
		d.RecentNotes = append(d.RecentNotes, map[string]any{"text": text, "at": at, "card_ref": ptr(card)})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	if d.Agent.CurrentCardRef != nil {
		var id, title, handle string
		e = resourceaccess.NewDB(s.DB).QueryRowContext(ctx, `SELECT id,title,handle FROM cards WHERE handle=? OR id=?`, strings.TrimPrefix(*d.Agent.CurrentCardRef, "card:"), strings.TrimPrefix(*d.Agent.CurrentCardRef, "card:")).Scan(&id, &title, &handle)
		if e == nil {
			d.RecentCards = append(d.RecentCards, map[string]any{"id": id, "ref": "card:" + handle, "handle": handle, "title": title})
		} else if !errors.Is(e, sql.ErrNoRows) {
			return d, e
		}
	}
	return d, nil
}
