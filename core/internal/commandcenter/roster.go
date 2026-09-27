package commandcenter

import (
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
	rows, e := s.DB.QueryContext(ctx, `SELECT a.id,a.username,a.actor_id,a.metadata_json,
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
		ORDER BY a.username`)
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
	identities, e := s.Identities.ListAgents(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]Summary, len(identities))
	index := map[string]int{}
	for i, v := range identities {
		out[i] = Summary{ID: v.AgentID, Ref: "actor:" + v.ActorID, ActorID: v.ActorID, HostID: stringPtr(v.HostID), HostSlug: stringPtr(v.HostSlug), Name: v.Name, Handle: v.Handle, DisplayName: v.DisplayName, IdentityKind: v.Kind, State: "stale", RevokedAt: stringPtr(v.RevokedAt), BridgeOnline: bridgeOnline(v.BridgeExpiresAt, now)}
		out[i].LastSignalAt = stringPtr(v.BridgeCheckedInAt)
		index[v.AgentID] = i
	}
	// Each source is scanned once. No per-agent event query is performed.
	rows, e := s.DB.QueryContext(ctx, `SELECT agent_id,current_card_ref,note,observed_at FROM agent_presence`)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id, at string
		var card, note sql.NullString
		if e = rows.Scan(&id, &card, &note, &at); e != nil {
			rows.Close()
			return nil, e
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
		return nil, e
	}
	runRows, e := s.DB.QueryContext(ctx, "SELECT "+runColumns+" FROM runs ORDER BY julianday(last_observed_at) DESC")
	if e != nil {
		return nil, e
	}
	for runRows.Next() {
		r, scanErr := readRun(runRows)
		if scanErr != nil {
			runRows.Close()
			return nil, scanErr
		}
		if i, ok := index[r.AgentID]; ok {
			v := &out[i]
			v.LastSignalAt = stringPtr(later(value(v.LastSignalAt), r.LastObservedAt))
			if active(r) && v.ActiveRun == nil {
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
		return nil, e
	}
	rows, e = s.DB.QueryContext(ctx, `SELECT actor_id,ts FROM (SELECT actor_id,ts,ROW_NUMBER() OVER (PARTITION BY actor_id ORDER BY julianday(ts) DESC,id DESC) AS row_number FROM events) WHERE row_number=1`)
	if e != nil {
		return nil, e
	}
	actorIndex := map[string]int{}
	for i, v := range out {
		actorIndex[v.ActorID] = i
	}
	for rows.Next() {
		var id, at string
		if e = rows.Scan(&id, &at); e != nil {
			rows.Close()
			return nil, e
		}
		if i, ok := actorIndex[id]; ok {
			out[i].LastSignalAt = stringPtr(later(value(out[i].LastSignalAt), at))
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	asks, e := s.openAsks(ctx)
	if e != nil {
		return nil, e
	}
	for _, ask := range asks {
		i, ok := index[value(ask.RequesterAgentID)]
		if !ok {
			i, ok = actorIndex[ask.RequesterActorID]
		}
		if ok {
			out[i].OpenAsksCount++
			if out[i].WaitingAsk == nil {
				copy := ask
				out[i].WaitingAsk = &copy
			}
		}
	}
	cardRows, e := s.DB.QueryContext(ctx, `SELECT id,handle,title FROM cards`)
	if e != nil {
		return nil, e
	}
	cardTitles := map[string]string{}
	for cardRows.Next() {
		var id, handle, title string
		if e = cardRows.Scan(&id, &handle, &title); e != nil {
			cardRows.Close()
			return nil, e
		}
		cardTitles[id] = title
		cardTitles[handle] = title
	}
	e = cardRows.Err()
	cardRows.Close()
	if e != nil {
		return nil, e
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
	return out, nil
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
	rows, e := s.DB.QueryContext(ctx, `SELECT req.id,req.ts,COALESCE(json_extract(req.payload_json,'$.summary'),''),req.payload_json,
		COALESCE((SELECT di.id FROM derived_inbox_items di WHERE di.source_event_id=req.id AND di.category IN ('ask','review','escalate') ORDER BY di.id LIMIT 1),''),
		COALESCE((SELECT di.data_json FROM derived_inbox_items di WHERE di.source_event_id=req.id AND di.category IN ('ask','review','escalate') ORDER BY di.id LIMIT 1),'')
		FROM events req WHERE req.type='human_attention_requested' AND req.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM events resp WHERE resp.type='human_attention_responded' AND resp.trashed_at IS NULL AND (json_extract(resp.payload_json,'$.payload.request_event_id')=req.id OR json_extract(resp.payload_json,'$.payload.request_event_ref')='event:'||req.id OR json_extract(resp.payload_json,'$.payload.request_event_ref')='event:'||req.handle))
		ORDER BY julianday(req.ts),req.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []OpenAsk{}
	for rows.Next() {
		var id, at, summary, raw, inboxID, inboxRaw string
		if e = rows.Scan(&id, &at, &summary, &raw, &inboxID, &inboxRaw); e != nil {
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
		ask := OpenAsk{ID: id, InboxItemID: stringPtr(inboxID), CreatedAt: at,
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
	rows, e := s.DB.QueryContext(ctx, `SELECT text,observed_at,card_ref FROM agent_progress_notes WHERE agent_id=? ORDER BY observed_at DESC LIMIT 50`, d.Agent.ID)
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
		e = s.DB.QueryRowContext(ctx, `SELECT id,title,handle FROM cards WHERE handle=? OR id=?`, strings.TrimPrefix(*d.Agent.CurrentCardRef, "card:"), strings.TrimPrefix(*d.Agent.CurrentCardRef, "card:")).Scan(&id, &title, &handle)
		if e == nil {
			d.RecentCards = append(d.RecentCards, map[string]any{"id": id, "ref": "card:" + handle, "handle": handle, "title": title})
		} else if !errors.Is(e, sql.ErrNoRows) {
			return d, e
		}
	}
	return d, nil
}
