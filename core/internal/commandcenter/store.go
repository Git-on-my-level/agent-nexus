package commandcenter

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not_found")
var ErrIdentityConflict = errors.New("run_identity_conflict")
var ErrStateRegression = errors.New("run_state_regression")
var ErrInvalid = errors.New("invalid_request")

const PresenceFreshness = 30 * time.Minute
const SignalStaleness = 24 * time.Hour
const BridgeFreshness = 5 * time.Minute
const RunObservationFutureSkew = 2 * time.Minute
const ActiveRunFreshness = 5 * time.Minute

var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

type Run struct {
	ID              string   `json:"id"`
	Ref             string   `json:"ref"`
	Handle          string   `json:"handle"`
	Launcher        string   `json:"launcher"`
	ExternalID      string   `json:"external_id"`
	HostID          string   `json:"host_id"`
	AgentID         string   `json:"agent_id"`
	Adapter         string   `json:"adapter"`
	Model           *string  `json:"model"`
	State           string   `json:"state"`
	Liveness        string   `json:"liveness"`
	ResultCollected bool     `json:"result_collected"`
	Labels          []string `json:"labels"`
	CardRef         *string  `json:"card_ref"`
	Repository      *string  `json:"repository"`
	Branch          *string  `json:"branch"`
	StartedAt       *string  `json:"started_at"`
	EndedAt         *string  `json:"ended_at"`
	LastObservedAt  string   `json:"last_observed_at"`
}

type Filter struct {
	CardRef, AgentID, HostID, State, Cursor string
	Active                                  *bool
	Limit                                   int
}
type Presence struct {
	AgentID        string  `json:"agent_id"`
	CurrentCardRef *string `json:"current_card_ref"`
	Note           *string `json:"note"`
	ObservedAt     string  `json:"observed_at"`
}
type PresencePatch struct {
	CardRef *string
	SetCard bool
	Note    *string
	SetNote bool
}
type Identity struct{ AgentID, ActorID, HostID, HostSlug, Name, Handle, DisplayName, Kind, Adapter, RevokedAt, BridgeCheckedInAt, BridgeExpiresAt string }
type IdentitySource interface {
	ResolveAgent(context.Context, string) (Identity, error)
	ListAgents(context.Context) ([]Identity, error)
}
type Store struct {
	DB         *sql.DB
	Identities IdentitySource
}

func NewStore(db *sql.DB, identities IdentitySource) *Store {
	return &Store{DB: db, Identities: identities}
}

func validState(s string) bool {
	switch s {
	case "unknown", "starting", "running", "completed", "failed", "cancelled":
		return true
	}
	return false
}
func terminal(s string) bool { return s == "completed" || s == "failed" || s == "cancelled" }
func activeAt(r Run, now time.Time) bool {
	if terminal(r.State) || r.Liveness != "alive" {
		return false
	}
	observed, err := parseTime(r.LastObservedAt)
	return err == nil && !observed.After(now.Add(RunObservationFutureSkew)) && now.Sub(observed) <= ActiveRunFreshness
}
func parseTime(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return time.Time{}, e
	}
	return t.UTC(), nil
}
func cardFromLabels(labels []string) (*string, error) {
	var card *string
	for _, label := range labels {
		if !strings.HasPrefix(label, "anx.card.") {
			continue
		}
		slug := strings.TrimPrefix(label, "anx.card.")
		if slug == "" || strings.ContainsAny(slug, " /\t\n") {
			return nil, ErrInvalid
		}
		if card != nil {
			return nil, ErrInvalid
		}
		v := "card:" + slug
		card = &v
	}
	return card, nil
}
func ValidateRun(r *Run) error {
	if r.Launcher != "agentctl" || r.ExternalID == "" || r.HostID == "" || r.AgentID == "" || !agentNamePattern.MatchString(r.Adapter) || !validState(r.State) || (r.Liveness != "alive" && r.Liveness != "stale" && r.Liveness != "unknown") {
		return ErrInvalid
	}
	observed, e := parseTime(r.LastObservedAt)
	if e != nil || observed.After(time.Now().UTC().Add(RunObservationFutureSkew)) {
		return ErrInvalid
	}
	for _, p := range []*string{r.StartedAt, r.EndedAt} {
		if p != nil {
			value, e := parseTime(*p)
			if e != nil || value.After(time.Now().UTC().Add(RunObservationFutureSkew)) {
				return ErrInvalid
			}
		}
	}
	if r.CardRef != nil {
		if !strings.HasPrefix(*r.CardRef, "card:") || len(*r.CardRef) <= 5 {
			return ErrInvalid
		}
	} else {
		v, e := cardFromLabels(r.Labels)
		if e != nil {
			return e
		}
		r.CardRef = v
	}
	if r.Labels == nil {
		r.Labels = []string{}
	}
	return nil
}
func readRun(row interface{ Scan(...any) error }) (Run, error) {
	var r Run
	var model, card, repo, branch, start, end sql.NullString
	var labels string
	var result int
	e := row.Scan(&r.ID, &r.Handle, &r.Launcher, &r.ExternalID, &r.HostID, &r.AgentID, &r.Adapter, &model, &r.State, &r.Liveness, &result, &labels, &card, &repo, &branch, &start, &end, &r.LastObservedAt)
	if e != nil {
		return r, e
	}
	r.Ref = "run:" + r.Handle
	r.ResultCollected = result != 0
	r.Model = ptr(model)
	r.CardRef = ptr(card)
	r.Repository = ptr(repo)
	r.Branch = ptr(branch)
	r.StartedAt = ptr(start)
	r.EndedAt = ptr(end)
	if e = json.Unmarshal([]byte(labels), &r.Labels); e != nil {
		return r, e
	}
	return r, nil
}
func ptr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

const runColumns = "id,handle,launcher,external_id,host_id,agent_id,adapter,model,state,liveness,result_collected,labels_json,card_ref,repository,branch,started_at,ended_at,last_observed_at"

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	r, e := readRun(s.DB.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE id=? OR handle=?", id, strings.TrimPrefix(id, "run:")))
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, e
}
func (s *Store) RunByExternal(ctx context.Context, launcher, host, external string) (Run, error) {
	r, e := readRun(s.DB.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE launcher=? AND host_id=? AND external_id=?", launcher, host, external))
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, e
}
func (s *Store) UpsertRun(ctx context.Context, in Run) (Run, bool, bool, error) {
	if e := ValidateRun(&in); e != nil {
		return Run{}, false, false, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Run{}, false, false, e
	}
	defer tx.Rollback()
	old, e := readRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE launcher=? AND host_id=? AND external_id=?", in.Launcher, in.HostID, in.ExternalID))
	if errors.Is(e, sql.ErrNoRows) {
		in.ID = uuid.NewString()
		in.Handle = in.ExternalID + "-" + in.ID[:8]
		in.Ref = "run:" + in.Handle
		labels, _ := json.Marshal(in.Labels)
		_, e = tx.ExecContext(ctx, `INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,model,state,liveness,result_collected,labels_json,card_ref,repository,branch,started_at,ended_at,last_observed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.ID, in.Handle, in.Launcher, in.ExternalID, in.HostID, in.AgentID, in.Adapter, in.Model, in.State, in.Liveness, in.ResultCollected, string(labels), in.CardRef, in.Repository, in.Branch, in.StartedAt, in.EndedAt, in.LastObservedAt)
		if e != nil {
			return Run{}, false, false, e
		}
		if e = tx.Commit(); e != nil {
			return Run{}, false, false, e
		}
		return in, true, false, nil
	}
	if e != nil {
		return Run{}, false, false, e
	}
	if old.AgentID != in.AgentID || (old.Adapter != in.Adapter && !(old.State == "unknown" && in.State != "unknown")) {
		return Run{}, false, false, ErrIdentityConflict
	}
	oldAt, _ := parseTime(old.LastObservedAt)
	newAt, _ := parseTime(in.LastObservedAt)
	if terminal(old.State) && old.State != in.State && terminal(in.State) {
		return Run{}, false, false, ErrStateRegression
	}
	if !terminal(old.State) && in.State != "unknown" && old.State != "unknown" && (in.State == "starting" && old.State != "starting" || in.State == "running" && terminal(old.State)) {
		return Run{}, false, false, ErrStateRegression
	}
	terminalIncoming := terminal(in.State) && !terminal(old.State)
	if newAt.Before(oldAt) && old.State != "unknown" && !terminalIncoming {
		if e = tx.Commit(); e != nil {
			return Run{}, false, false, e
		}
		return old, false, true, nil
	}
	merged := old
	if old.State == "unknown" && in.State != "unknown" {
		merged.Adapter = in.Adapter
	}
	if !terminal(old.State) && in.State != "unknown" {
		if !(old.State == "running" && in.State == "starting") {
			merged.State = in.State
		}
	}
	if terminalIncoming {
		merged.Liveness = in.Liveness
		merged.ResultCollected = old.ResultCollected || in.ResultCollected
		if in.EndedAt != nil {
			merged.EndedAt = in.EndedAt
		} else if merged.EndedAt == nil {
			merged.EndedAt = &in.LastObservedAt
		}
	}
	if !newAt.Before(oldAt) || old.State == "unknown" {
		if !newAt.Before(oldAt) {
			merged.LastObservedAt = in.LastObservedAt
		}
		if !newAt.Before(oldAt) || old.Liveness == "unknown" {
			merged.Liveness = in.Liveness
		}
		merged.ResultCollected = old.ResultCollected || in.ResultCollected
		if in.Model != nil {
			merged.Model = in.Model
		}
		if in.Repository != nil {
			merged.Repository = in.Repository
		}
		if in.Branch != nil {
			merged.Branch = in.Branch
		}
		if in.CardRef != nil {
			merged.CardRef = in.CardRef
		}
		if len(in.Labels) > 0 {
			merged.Labels = in.Labels
		}
	}
	if in.StartedAt != nil {
		incomingStart, _ := parseTime(*in.StartedAt)
		if merged.StartedAt == nil {
			merged.StartedAt = in.StartedAt
		} else if currentStart, err := parseTime(*merged.StartedAt); err == nil && incomingStart.Before(currentStart) {
			merged.StartedAt = in.StartedAt
		}
	}
	if terminal(merged.State) && merged.EndedAt == nil && in.EndedAt != nil {
		merged.EndedAt = in.EndedAt
	}
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(merged)
	replayed := string(before) == string(after)
	if !replayed {
		labels, _ := json.Marshal(merged.Labels)
		_, e = tx.ExecContext(ctx, `UPDATE runs SET adapter=?,model=?,state=?,liveness=?,result_collected=?,labels_json=?,card_ref=?,repository=?,branch=?,started_at=?,ended_at=?,last_observed_at=? WHERE id=?`, merged.Adapter, merged.Model, merged.State, merged.Liveness, merged.ResultCollected, string(labels), merged.CardRef, merged.Repository, merged.Branch, merged.StartedAt, merged.EndedAt, merged.LastObservedAt, merged.ID)
		if e != nil {
			return Run{}, false, false, e
		}
	}
	if e = tx.Commit(); e != nil {
		return Run{}, false, false, e
	}
	return merged, false, replayed, nil
}
func (s *Store) Provisional(ctx context.Context, identity Identity, external string) (Run, error) {
	old, e := s.RunByExternal(ctx, "agentctl", identity.HostID, external)
	if e == nil {
		if old.AgentID != identity.AgentID {
			return Run{}, ErrIdentityConflict
		}
		return old, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return Run{}, e
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r, _, _, e := s.UpsertRun(ctx, Run{Launcher: "agentctl", ExternalID: external, HostID: identity.HostID, AgentID: identity.AgentID, Adapter: identity.Adapter, State: "unknown", Liveness: "unknown", Labels: []string{}, LastObservedAt: now})
	return r, e
}
func (s *Store) ListRuns(ctx context.Context, f Filter) ([]Run, string, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		return nil, "", ErrInvalid
	}
	q := "SELECT " + runColumns + " FROM runs WHERE 1=1"
	args := []any{}
	for _, item := range []struct{ col, val string }{{"card_ref", f.CardRef}, {"agent_id", f.AgentID}, {"host_id", f.HostID}, {"state", f.State}} {
		if item.val != "" {
			q += " AND " + item.col + "=?"
			args = append(args, item.val)
		}
	}
	if f.Active != nil {
		if *f.Active {
			q += " AND state NOT IN ('completed','failed','cancelled') AND liveness='alive'"
		} else {
			q += " AND (state IN ('completed','failed','cancelled') OR liveness<>'alive')"
		}
	}
	if f.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		if e != nil {
			return nil, "", ErrInvalid
		}
		var c [2]string
		if e = json.Unmarshal(b, &c); e != nil || c[0] == "" || c[1] == "" {
			return nil, "", ErrInvalid
		}
		q += " AND (julianday(COALESCE(started_at,last_observed_at)),id)<(julianday(?),?)"
		args = append(args, c[0], c[1])
	}
	q += " ORDER BY julianday(COALESCE(started_at,last_observed_at)) DESC,id DESC LIMIT ?"
	args = append(args, f.Limit+1)
	rows, e := s.DB.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, e := readRun(rows)
		if e != nil {
			return nil, "", e
		}
		out = append(out, r)
	}
	if e = rows.Err(); e != nil {
		return nil, "", e
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		last := out[len(out)-1]
		key := last.LastObservedAt
		if last.StartedAt != nil {
			key = *last.StartedAt
		}
		b, _ := json.Marshal([2]string{key, last.ID})
		next = base64.RawURLEncoding.EncodeToString(b)
	}
	return out, next, nil
}
func (s *Store) PatchPresence(ctx context.Context, agentID string, p PresencePatch, now time.Time) (Presence, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Presence{}, e
	}
	defer tx.Rollback()
	var card, note sql.NullString
	e = tx.QueryRowContext(ctx, "SELECT current_card_ref,note FROM agent_presence WHERE agent_id=?", agentID).Scan(&card, &note)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Presence{}, e
	}
	if p.SetCard {
		card = sql.NullString{}
		if p.CardRef != nil {
			card = sql.NullString{String: *p.CardRef, Valid: true}
		}
	}
	if p.SetNote {
		note = sql.NullString{}
		if p.Note != nil {
			note = sql.NullString{String: *p.Note, Valid: true}
		}
	}
	at := now.UTC().Format(time.RFC3339Nano)
	_, e = tx.ExecContext(ctx, `INSERT INTO agent_presence(agent_id,current_card_ref,note,observed_at) VALUES(?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET current_card_ref=excluded.current_card_ref,note=excluded.note,observed_at=excluded.observed_at`, agentID, card, note, at)
	if e != nil {
		return Presence{}, e
	}
	if p.SetNote && p.Note != nil && *p.Note != "" {
		_, e = tx.ExecContext(ctx, "INSERT INTO agent_progress_notes(agent_id,card_ref,text,observed_at) VALUES(?,?,?,?)", agentID, card, *p.Note, at)
		if e != nil {
			return Presence{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return Presence{}, e
	}
	return Presence{agentID, ptr(card), ptr(note), at}, nil
}
func (s *Store) Presence(ctx context.Context, agentID string) (Presence, error) {
	var p Presence
	var card, note sql.NullString
	e := s.DB.QueryRowContext(ctx, "SELECT current_card_ref,note,observed_at FROM agent_presence WHERE agent_id=?", agentID).Scan(&card, &note, &p.ObservedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.AgentID = agentID
	p.CurrentCardRef = ptr(card)
	p.Note = ptr(note)
	return p, e
}
func (s *Store) Identity(ctx context.Context, id string) (Identity, error) {
	if s.Identities == nil {
		return Identity{}, fmt.Errorf("identity source unavailable")
	}
	return s.Identities.ResolveAgent(ctx, id)
}
