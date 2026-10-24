package primitives

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The cohort is a hard admission bound, shared by create hints, the ungrouped
// tray and orient. Explicit related parents are admitted separately by identity.
const homeCandidateLimit = 200

type LikelyHome struct {
	Ref     string   `json:"ref"`
	Title   string   `json:"title"`
	Reasons []string `json:"reasons"`
}

var numberedFamily = regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9 -]{0,40}?)\s+\d+/\d+(?:\b|:)`)
var laneFamily = regexp.MustCompile(`(?i)^\s*([a-z][a-z0-9]*(?:-[a-z0-9]+)*\d+)(?:\s|:|/|\b)`)

// TitleFamily is the single deterministic prefix implementation used by both
// likely-home and fold suggestions. It does not interpret arbitrary prose.
func TitleFamily(title string) string {
	if m := numberedFamily.FindStringSubmatch(title); m != nil {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	if m := laneFamily.FindStringSubmatch(title); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

type homeCandidate struct {
	id, ref, title, created, project, refs, metadata, updated, phase string
	signals, repositories                                            map[string]bool
	family                                                           string
}

func recentHomeCandidates(ctx context.Context, q briefQueryer, at ...time.Time) ([]homeCandidate, error) {
	now := time.Now().UTC()
	if len(at) > 0 {
		now = at[0]
	}
	return recentGroupingCandidates(ctx, q, true, now)
}

func recentGroupingCandidates(ctx context.Context, q briefQueryer, openOnly bool, now time.Time) ([]homeCandidate, error) {
	recent := `SELECT id FROM work_grouping_card_positions WHERE column_key NOT IN ('done','cancelled') ORDER BY created_at DESC,id DESC LIMIT 200`
	if !openOnly {
		recent = `SELECT id FROM (
		 SELECT id,created_at FROM (SELECT id,created_at FROM work_grouping_card_positions WHERE column_key NOT IN ('done','cancelled') ORDER BY created_at DESC,id DESC LIMIT 200)
		 UNION ALL SELECT id,created_at FROM (SELECT id,created_at FROM work_grouping_card_positions WHERE column_key IN ('done','cancelled') ORDER BY created_at DESC,id DESC LIMIT 200)
		 ORDER BY created_at DESC,id DESC LIMIT 200)`
	}
	// Admission precedes metadata decoding, hierarchy and matching. This is
	// deliberately a physical routing cohort before scoped hydration, never an
	// all-workspace similarity or denial scan. Routing keys never leave this query.
	rows, err := q.QueryContext(ctx, `SELECT c.id,'card:'||COALESCE(NULLIF(c.handle,''),c.id),c.title,c.created_at,c.updated_at,c.refs_json,
	 COALESCE(json_extract(m.metadata_json,'$.project_ref'),''),COALESCE(m.metadata_json,'{}'),c.column_key
	 FROM (`+recent+`) recent
	 CROSS JOIN cards c ON c.id=recent.id CROSS JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id
	 WHERE b.archived_at IS NULL AND b.trashed_at IS NULL AND NOT `+workHasParentSQL("c")+`
	 AND COALESCE(json_type(m.metadata_json,'$.cadence'),'null')='null' AND COALESCE(json_type(m.metadata_json,'$.goal'),'null')='null'
	 AND NOT EXISTS(SELECT 1 FROM brief_initiatives bi WHERE bi.card_id=c.id AND (bi.loop_annotated=1 OR bi.loop_last_run_at>=?))
	 ORDER BY c.created_at DESC,c.id DESC`, loopTS(now.Add(-loopWindow)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []homeCandidate{}
	for rows.Next() {
		var c homeCandidate
		if err = rows.Scan(&c.id, &c.ref, &c.title, &c.created, &c.updated, &c.refs, &c.project, &c.metadata, &c.phase); err != nil {
			return nil, err
		}
		out = append(out, prepareHomeCandidate(c))
	}
	return out, rows.Err()
}

func stringSet(raw any) map[string]bool {
	data, _ := json.Marshal(raw)
	var values []string
	_ = json.Unmarshal(data, &values)
	out := map[string]bool{}
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out[v] = true
		}
	}
	return out
}
func homeSignals(card map[string]any) (map[string]bool, map[string]bool) {
	refs := stringSet(card["related_refs"])
	for r := range stringSet(card["refs"]) {
		refs[r] = true
	}
	if r := workString(workMap(card["source"])["url"]); r != "" {
		refs[r] = true
	}
	data, _ := json.Marshal(card["source_refs"])
	var sources []map[string]any
	_ = json.Unmarshal(data, &sources)
	for _, source := range sources {
		for _, k := range []string{"url", "ref"} {
			if r := workString(source[k]); r != "" {
				refs[r] = true
			}
		}
	}
	repos := map[string]bool{}
	for r := range refs {
		u, err := url.Parse(r)
		if err != nil || u.Host == "" || u.User != nil {
			continue
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		// Repository identity is generic: an explicit repository field or a
		// common forge URL. No network request or provider credential is used.
		if (strings.EqualFold(u.Hostname(), "github.com") || strings.EqualFold(u.Hostname(), "gitlab.com")) && len(parts) >= 2 {
			repos[strings.ToLower(u.Hostname()+"/"+parts[0]+"/"+parts[1])] = true
		}
	}
	if repo := workString(workMap(card["source"])["repository"]); repo != "" {
		repos[strings.ToLower(repo)] = true
	}
	return refs, repos
}
func intersects(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

func likelyHomes(card map[string]any, candidates []homeCandidate) []LikelyHome {
	refs, repos := homeSignals(card)
	related := stringSet(card["related_refs"])
	family := TitleFamily(workString(card["title"]))
	type scored struct {
		home        LikelyHome
		score       int
		created, id string
	}
	scores := []scored{}
	for _, c := range candidates {
		if c.ref == card["ref"] || c.id == card["id"] || c.phase == "done" || c.phase == "cancelled" {
			continue
		}
		r, rp := c.signals, c.repositories
		reasons := []string{}
		score := 0
		if related[c.ref] || related["card:"+c.id] {
			reasons = append(reasons, "related_parent")
			score += 1000
		}
		if intersects(refs, r) {
			reasons = append(reasons, "shared_ref")
			score += 100
		}
		if intersects(repos, rp) {
			reasons = append(reasons, "same_repository")
			score += 30
		}
		if project := workString(card["project_ref"]); project != "" && project == c.project {
			reasons = append(reasons, "same_project")
			score += 20
		}
		if family != "" && family == c.family {
			reasons = append(reasons, "title_family")
			score += 10
		}
		if score > 0 {
			scores = append(scores, scored{LikelyHome{c.ref, c.title, reasons}, score, c.created, c.id})
		}
	}
	sort.Slice(scores, func(i, j int) bool {
		a, b := scores[i], scores[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.created != b.created {
			return a.created < b.created
		}
		return a.id < b.id
	})
	out := []LikelyHome{}
	for _, s := range scores {
		out = append(out, s.home)
		if len(out) == 3 {
			break
		}
	}
	return out
}

// Explicit related parents win even when older than the recent cohort. The
// identity batch is capped at 200; identity seeks retain scoped visibility.
func homeCandidatesFor(ctx context.Context, q briefQueryer, card map[string]any, base []homeCandidate) ([]homeCandidate, error) {
	refs := []string{}
	for r := range stringSet(card["related_refs"]) {
		if strings.HasPrefix(r, "card:") {
			refs = append(refs, strings.TrimPrefix(r, "card:"))
		}
	}
	if len(refs) == 0 {
		return base, nil
	}
	sort.Strings(refs)
	if len(refs) > 200 {
		refs = refs[:200]
	}
	encoded, _ := json.Marshal(refs)
	rows, err := q.QueryContext(ctx, `SELECT c.id,'card:'||COALESCE(NULLIF(c.handle,''),c.id),c.title,c.created_at,c.updated_at,c.refs_json,COALESCE(json_extract(m.metadata_json,'$.project_ref'),''),COALESCE(m.metadata_json,'{}')
	 FROM json_each(?) requested CROSS JOIN cards c ON c.id=requested.value OR (c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle=requested.value) CROSS JOIN boards b ON b.id=c.board_id LEFT JOIN work_metadata m ON m.card_id=c.id
	 WHERE c.archived_at IS NULL AND c.trashed_at IS NULL AND c.column_key NOT IN ('done','cancelled') AND b.archived_at IS NULL AND b.trashed_at IS NULL
	 AND NOT `+workHasParentSQL("c")+` AND COALESCE(json_type(m.metadata_json,'$.cadence'),'null')='null' AND COALESCE(json_type(m.metadata_json,'$.goal'),'null')='null' AND NOT EXISTS(SELECT 1 FROM brief_initiatives bi WHERE bi.card_id=c.id AND (bi.loop_annotated=1 OR bi.loop_last_run_at>=?)) LIMIT 200`, string(encoded), loopTS(time.Now().UTC().Add(-loopWindow)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := append([]homeCandidate{}, base...)
	seen := map[string]bool{}
	for _, c := range out {
		seen[c.id] = true
	}
	for rows.Next() {
		var c homeCandidate
		if err = rows.Scan(&c.id, &c.ref, &c.title, &c.created, &c.updated, &c.refs, &c.project, &c.metadata); err != nil {
			return nil, err
		}
		if !seen[c.id] {
			seen[c.id] = true
			out = append(out, prepareHomeCandidate(c))
		}
	}
	return out, rows.Err()
}

func decorateCreatedCard(ctx context.Context, q briefQueryer, card, m map[string]any) error {
	card["parent_ref"] = nil
	if under := workString(m["under"]); under != "" {
		card["parent_ref"] = under
		card["likely_home"] = []LikelyHome{}
		return nil
	}
	base, err := recentHomeCandidates(ctx, q)
	if err != nil {
		return err
	}
	input := workClone(m)
	for k, v := range card {
		if _, ok := input[k]; !ok {
			input[k] = v
		}
	}
	candidates, err := homeCandidatesFor(ctx, q, input, base)
	if err != nil {
		return err
	}
	card["likely_home"] = likelyHomes(input, candidates)
	return nil
}

func GroupingNextActions(ref string, homes []LikelyHome, verb string) []map[string]any {
	out := []map[string]any{}
	if len(homes) == 0 {
		return out
	}
	flag := "--under"
	if verb == "fold" {
		flag = "--into"
	}
	return append(out, map[string]any{"label": "Choose a home", "argv": []string{"anx", "work", verb, ref, flag, homes[0].Ref}, "mutates": true, "side_effect_class": "remote_coordination_write", "reasons": homes[0].Reasons})
}

func (s *Store) UngroupedWork(ctx context.Context, limit int) ([]map[string]any, bool, error) {
	if limit < 1 || limit > 50 {
		return nil, false, workInvalid("limit must be 1..50")
	}
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return nil, false, err
	}
	defer closeRead()
	base, err := recentGroupingCandidates(ctx, s.db, false, time.Now().UTC())
	if err != nil {
		return nil, false, err
	}
	more := len(base) > limit
	candidates := base[:min(limit, len(base))]
	inputs := []map[string]any{}
	related := []string{}
	for _, c := range candidates {
		var m map[string]any
		_ = json.Unmarshal([]byte(c.metadata), &m)
		if m == nil {
			m = map[string]any{}
		}
		m["id"], m["ref"], m["title"], m["refs"] = c.id, c.ref, c.title, json.RawMessage(c.refs)
		for r := range stringSet(m["related_refs"]) {
			related = append(related, r)
		}
		for r := range stringSet(m["refs"]) {
			if r != "card:"+c.id && r != c.ref {
				related = append(related, r)
			}
		}
		inputs = append(inputs, m)
	}
	all, err := homeCandidatesFor(ctx, s.db, map[string]any{"related_refs": related}, base)
	if err != nil {
		return nil, false, err
	}
	out := []map[string]any{}
	for i, c := range candidates {
		homes := likelyHomes(inputs[i], all)
		out = append(out, map[string]any{"ref": c.ref, "title": c.title, "phase": c.phase, "parent_ref": nil, "created_at": c.created, "likely_home": homes, "next_actions": GroupingNextActions(c.ref, homes, "group")})
	}

	return out, more, nil
}

func staleHome(c homeCandidate, now time.Time) bool {
	at, err := time.Parse(time.RFC3339Nano, c.updated)
	return err == nil && now.Sub(at) > 7*24*time.Hour
}

func prepareHomeCandidate(c homeCandidate) homeCandidate {
	var m map[string]any
	_ = json.Unmarshal([]byte(c.metadata), &m)
	if m == nil {
		m = map[string]any{}
	}
	m["refs"] = json.RawMessage(c.refs)
	c.signals, c.repositories = homeSignals(m)
	c.family = TitleFamily(c.title)
	return c
}
