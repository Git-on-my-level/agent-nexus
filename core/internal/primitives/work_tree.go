package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Paths are display and input aliases over stable card IDs. Each segment is a
// card's handle (or its ID when it has none); initiatives live under
// "initiatives/" and the Unsorted tray under "unsorted/". Stored refs always
// keep card IDs, so a renamed handle never corrupts data.
const (
	workTreeInitiativesRoot = "initiatives"
	workTreeUnsortedRoot    = "unsorted"
	workTreeNodeLimit       = 200
)

type workTreeRow struct {
	id, handle, title, state, assignee string
	parent                             sql.NullString
	needsHome                          bool
}

func (r workTreeRow) segment() string { return firstNonEmptyString(r.handle, r.id) }

func scanWorkTreeRows(rows *sql.Rows) ([]workTreeRow, error) {
	defer rows.Close()
	out := []workTreeRow{}
	for rows.Next() {
		var r workTreeRow
		var handle, assignee sql.NullString
		if err := rows.Scan(&r.id, &handle, &r.title, &r.state, &assignee, &r.parent, &r.needsHome); err != nil {
			return nil, err
		}
		r.handle, r.assignee = strings.TrimSpace(handle.String), assignee.String
		out = append(out, r)
	}
	return out, rows.Err()
}

const workTreeColumns = `c.id,c.handle,c.title,c.column_key,c.assignee,c.parent_card_id,c.needs_home`

func workTreeOpen(includeDone bool) string {
	if includeDone {
		return ""
	}
	return ` AND c.column_key NOT IN ('done','cancelled')`
}

// workPathFor walks visible ancestors. A hidden ancestor yields no path.
func workPathFor(ctx context.Context, q briefQueryer, id string) (string, error) {
	segments := []string{}
	for at, steps := id, 0; at != ""; steps++ {
		if steps > 32 {
			return "", nil
		}
		rows, err := q.QueryContext(ctx, `SELECT `+workTreeColumns+` FROM cards c WHERE c.id=? AND c.trashed_at IS NULL`, at)
		if err != nil {
			return "", err
		}
		found, err := scanWorkTreeRows(rows)
		if err != nil {
			return "", err
		}
		if len(found) == 0 {
			return "", nil
		}
		r := found[0]
		segments = append([]string{r.segment()}, segments...)
		if !r.parent.Valid {
			root := workTreeInitiativesRoot
			if r.needsHome {
				root = workTreeUnsortedRoot
			}
			return root + "/" + strings.Join(segments, "/"), nil
		}
		at = r.parent.String
	}
	return "", nil
}

func (s *Store) WorkPath(ctx context.Context, id string) (string, error) {
	return workPathFor(ctx, s.db, id)
}

// ResolveWorkPath maps a path alias to its card ref.
func (s *Store) ResolveWorkPath(ctx context.Context, path string) (map[string]any, error) {
	segments := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(segments) < 2 || (segments[0] != workTreeInitiativesRoot && segments[0] != workTreeUnsortedRoot) {
		return nil, workInvalid("a work path starts with initiatives/ or unsorted/")
	}
	if len(segments) > workTreeMaxDepth+1 {
		return nil, ErrNotFound
	}
	parent := ""
	var last workTreeRow
	for i, segment := range segments[1:] {
		where := `c.parent_card_id=?`
		args := []any{parent}
		if i == 0 {
			where = `c.parent_card_id IS NULL AND c.needs_home=?`
			args = []any{segments[0] == workTreeUnsortedRoot}
		}
		args = append([]any{segment, segment}, args...)
		rows, err := s.db.QueryContext(ctx, `SELECT `+workTreeColumns+` FROM (SELECT id FROM cards WHERE id=? UNION SELECT id FROM cards WHERE handle IS NOT NULL AND trim(handle)<>'' AND handle=?) requested CROSS JOIN cards c ON c.id=requested.id WHERE `+where+` AND c.trashed_at IS NULL ORDER BY c.created_at,c.id LIMIT 1`, args...)
		if err != nil {
			return nil, err
		}
		found, err := scanWorkTreeRows(rows)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, ErrNotFound
		}
		last = found[0]
		parent = last.id
	}
	return map[string]any{"ref": "card:" + last.segment(), "path": segments[0] + "/" + strings.Join(segments[1:], "/")}, nil
}

// WorkTree reads initiatives and the Unsorted tray, or one subtree, at most
// depth levels deep (1..3) and workTreeNodeLimit nodes per level.
func (s *Store) WorkTree(ctx context.Context, root string, depth int, includeDone bool) (map[string]any, error) {
	if depth == 0 {
		depth = 2
	}
	if depth < 1 || depth > workTreeMaxDepth {
		return nil, workInvalid("depth must be 1..%d", workTreeMaxDepth)
	}
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()
	truncated := false
	// Admit routing identities before scoped canonical hydration. Hidden rows
	// never cause a refill or contribute to returned truncation metadata.
	level := func(index, where string, args ...any) ([]workTreeRow, error) {
		// Root/tray partial indexes require the live predicate. Other lifecycle
		// eligibility follows the fixed routing window, without scanning to refill.
		live, order := "", `c.parent_card_id,c.created_at,c.id`
		if index != "idx_cards_parent" {
			live, order = ` AND c.trashed_at IS NULL`, `c.created_at,c.id`
		}
		rows, err := s.db.QueryContext(ctx, `SELECT `+workTreeColumns+` FROM (SELECT c.id FROM main.cards c INDEXED BY `+index+` WHERE `+where+live+` ORDER BY `+order+` LIMIT ?) admitted CROSS JOIN cards c ON c.id=admitted.id WHERE c.trashed_at IS NULL AND c.archived_at IS NULL`+workTreeOpen(includeDone)+` ORDER BY c.created_at,c.id`, append(args, workTreeNodeLimit+1)...)
		if err != nil {
			return nil, err
		}
		found, err := scanWorkTreeRows(rows)
		if len(found) > workTreeNodeLimit {
			found, truncated = found[:workTreeNodeLimit], true
		}
		return found, err
	}
	node := func(r workTreeRow, path string) map[string]any {
		var owner any
		if refs := boardCardAssigneeRefs(r.assignee); len(refs) > 0 {
			owner = refs[0]
		}
		return map[string]any{"ref": "card:" + r.segment(), "title": r.title, "state": r.state, "path": path, "owner": owner, "needs_home": r.needsHome, "is_initiative": !r.parent.Valid && !r.needsHome, "children": []map[string]any{}, "has_more": false}
	}
	// Expand children breadth-first: one indexed query per level, never per node.
	expand := func(roots []map[string]any, ids []string, levels int) error {
		byID := map[string]map[string]any{}
		for i, id := range ids {
			byID[id] = roots[i]
		}
		for ; levels > 1 && len(byID) > 0; levels-- {
			keys := make([]string, 0, len(byID))
			for id := range byID {
				keys = append(keys, id)
			}
			sort.Strings(keys)
			encoded, _ := json.Marshal(keys)
			children, err := level("idx_cards_parent", `c.parent_card_id IN (SELECT value FROM json_each(?))`, string(encoded))
			if err != nil {
				return err
			}
			next := map[string]map[string]any{}
			for _, child := range children {
				parent := byID[child.parent.String]
				if parent == nil {
					continue
				}
				n := node(child, workString(parent["path"])+"/"+child.segment())
				parent["children"] = append(parent["children"].([]map[string]any), n)
				next[child.id] = n
			}
			byID = next
		}
		// Leaves at the depth limit report whether they have hidden children.
		if len(byID) > 0 {
			keys := make([]string, 0, len(byID))
			for id := range byID {
				keys = append(keys, id)
			}
			encoded, _ := json.Marshal(keys)
			rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT c.parent_card_id FROM cards c WHERE c.parent_card_id IN (SELECT value FROM json_each(?)) AND c.trashed_at IS NULL AND c.archived_at IS NULL`+workTreeOpen(includeDone), string(encoded))
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					return err
				}
				byID[id]["has_more"] = true
			}
			return rows.Err()
		}
		return nil
	}
	if strings.TrimSpace(root) != "" {
		resolved, err := s.resolveWorkTreeRoot(ctx, root)
		if err != nil {
			return nil, err
		}
		rows, err := s.db.QueryContext(ctx, `SELECT `+workTreeColumns+` FROM cards c WHERE c.id=? AND c.trashed_at IS NULL`, resolved)
		if err != nil {
			return nil, err
		}
		found, err := scanWorkTreeRows(rows)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, ErrNotFound
		}
		path, err := workPathFor(ctx, s.db, resolved)
		if err != nil {
			return nil, err
		}
		n := node(found[0], path)
		if err = expand([]map[string]any{n}, []string{resolved}, depth); err != nil {
			return nil, err
		}
		return map[string]any{"node": n, "truncated": truncated}, nil
	}
	out := map[string]any{}
	for _, section := range []struct {
		key, root string
		tray      bool
	}{{"initiatives", workTreeInitiativesRoot, false}, {"unsorted", workTreeUnsortedRoot, true}} {
		index, where := "idx_cards_tree_roots", `c.parent_card_id IS NULL AND c.needs_home=0`
		if section.tray {
			index, where = "idx_cards_tree_tray", `c.parent_card_id IS NULL AND c.needs_home=1`
		}
		rows, err := level(index, where)
		if err != nil {
			return nil, err
		}
		nodes, ids := []map[string]any{}, []string{}
		for _, r := range rows {
			nodes = append(nodes, node(r, section.root+"/"+r.segment()))
			ids = append(ids, r.id)
		}
		levels := depth
		if section.tray {
			levels = 1 // Unsorted work is listed flat until it has a home.
		}
		if err = expand(nodes, ids, levels); err != nil {
			return nil, err
		}
		out[section.key] = nodes
	}
	out["truncated"] = truncated
	return out, nil
}

func (s *Store) resolveWorkTreeRoot(ctx context.Context, root string) (string, error) {
	root = strings.TrimSpace(root)
	if !strings.HasPrefix(root, "card:") && strings.Contains(root, "/") {
		resolved, err := s.ResolveWorkPath(ctx, root)
		if err != nil {
			return "", err
		}
		root = workString(resolved["ref"])
	}
	resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: root})
	if err != nil {
		return "", err
	}
	return resolved.ID, nil
}

// WorkWhere explains where work sits and suggests homes. Suggestions are
// advisory only: the caller confirms one by moving the card explicitly.
func (s *Store) WorkWhere(ctx context.Context, ref, query string) (map[string]any, error) {
	ref, query = strings.TrimSpace(ref), strings.TrimSpace(query)
	if (ref == "") == (query == "") {
		return nil, workInvalid("where needs exactly one of ref or q")
	}
	out := map[string]any{"ref": nil, "path": nil, "parent_ref": nil, "is_initiative": false, "needs_home": false, "suggestions": []map[string]any{}, "next_actions": []map[string]any{}}
	input := map[string]any{"title": query}
	cardRef, cardID := "", ""
	if ref != "" {
		id, err := s.resolveWorkTreeRoot(ctx, ref)
		if err != nil {
			return nil, err
		}
		work, err := s.GetWork(ctx, id)
		if err != nil {
			return nil, err
		}
		cardID, cardRef, input = id, workString(work["ref"]), work
		path, err := s.WorkPath(ctx, id)
		if err != nil {
			return nil, err
		}
		var parent sql.NullString
		var needsHome bool
		if err = s.db.QueryRowContext(ctx, `SELECT c.parent_card_id,c.needs_home FROM cards c WHERE c.id=?`, id).Scan(&parent, &needsHome); err != nil {
			return nil, err
		}
		out["ref"], out["needs_home"], out["is_initiative"] = cardRef, needsHome, !parent.Valid && !needsHome
		if path != "" {
			out["path"] = path
		}
		if parent.Valid {
			var parentRef string
			if err = s.db.QueryRowContext(ctx, `SELECT 'card:'||COALESCE(NULLIF(c.handle,''),c.id) FROM cards c WHERE c.id=?`, parent.String).Scan(&parentRef); err == nil {
				out["parent_ref"] = parentRef
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		if parent.Valid || !needsHome {
			return out, nil // Already filed; nothing to suggest.
		}
	}
	candidates, err := initiativeHomeCandidates(ctx, s.db)
	if err != nil {
		return nil, err
	}
	homes := likelyHomes(input, candidates)
	if len(homes) == 0 {
		homes = titleWordHomes(workString(input["title"]), candidates, cardID)
	}
	suggestions := []map[string]any{}
	for _, home := range homes[:min(5, len(homes))] {
		suggestion := map[string]any{"ref": home.Ref, "title": home.Title, "reasons": home.Reasons, "path": nil}
		for _, c := range candidates {
			if c.ref == home.Ref {
				if path, err := s.WorkPath(ctx, c.id); err == nil && path != "" {
					suggestion["path"] = path
				}
			}
		}
		suggestions = append(suggestions, suggestion)
	}
	out["suggestions"] = suggestions
	target := firstNonEmptyString(cardRef, "<card>")
	actions := []map[string]any{}
	if len(suggestions) > 0 {
		actions = append(actions, map[string]any{"label": "File it under " + workString(suggestions[0]["title"]), "command_id": "work.move", "argv": []string{"anx", "work", "move", target, "--under", workString(suggestions[0]["ref"])}, "mutates": true, "side_effect_class": "remote_coordination_write"})
	}
	if cardRef != "" {
		actions = append(actions, map[string]any{"label": "Make it an initiative", "command_id": "work.move", "argv": []string{"anx", "work", "move", target, "--root", "--reason", "<why this is a separate outcome>"}, "mutates": true, "side_effect_class": "remote_coordination_write"})
	}
	out["next_actions"] = actions
	return out, nil
}

// initiativeHomeCandidates admits at most 200 open initiatives.
func initiativeHomeCandidates(ctx context.Context, q briefQueryer) ([]homeCandidate, error) {
	rows, err := q.QueryContext(ctx, `SELECT c.id,'card:'||COALESCE(NULLIF(c.handle,''),c.id),c.title,c.created_at,c.updated_at,c.refs_json,COALESCE(json_extract(m.metadata_json,'$.project_ref'),''),COALESCE(m.metadata_json,'{}')
	 FROM (SELECT id FROM initiative_card_positions LIMIT 200) p CROSS JOIN cards c ON c.id=p.id LEFT JOIN work_metadata m ON m.card_id=c.id
	 WHERE c.archived_at IS NULL AND c.trashed_at IS NULL ORDER BY c.created_at,c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []homeCandidate{}
	for rows.Next() {
		var c homeCandidate
		if err = rows.Scan(&c.id, &c.ref, &c.title, &c.created, &c.updated, &c.refs, &c.project, &c.metadata); err != nil {
			return nil, err
		}
		c.declaredRoot = true
		out = append(out, prepareHomeCandidate(c))
	}
	return out, rows.Err()
}

var workWhereStopWords = map[string]bool{"about": true, "after": true, "again": true, "from": true, "into": true, "that": true, "their": true, "there": true, "these": true, "this": true, "with": true, "when": true, "where": true, "which": true, "while": true, "what": true, "work": true, "make": true, "should": true, "would": true, "could": true}

func workWhereWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(field) >= 4 && !workWhereStopWords[field] {
			words[strings.TrimSuffix(field, "s")] = true
		}
	}
	return words
}

// titleWordHomes ranks initiatives by shared significant title words when no
// structural signal (refs, repositories, families) matched.
func titleWordHomes(title string, candidates []homeCandidate, exclude string) []LikelyHome {
	words := workWhereWords(title)
	if len(words) == 0 {
		return []LikelyHome{}
	}
	type scored struct {
		home  LikelyHome
		score int
		at    string
	}
	ranked := []scored{}
	for _, c := range candidates {
		if c.id == exclude {
			continue
		}
		score := 0
		for word := range workWhereWords(c.title) {
			if words[word] {
				score++
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{LikelyHome{Ref: c.ref, Title: c.title, Reasons: []string{"title_words"}}, score, c.updated})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].at > ranked[j].at
	})
	out := []LikelyHome{}
	for _, r := range ranked {
		out = append(out, r.home)
	}
	return out
}
