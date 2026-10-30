package primitives

import (
	"context"
	"encoding/json"
	"strings"
)

// ReportActivitySubjects reuses the refs/resolve fact projection without plan,
// attention or metric enrichment. Each batch has at most 200 admitted subjects.
// Promise notes often carry only their backing thread; resolve those owners in
// one indexed batch. Missing, private and ambiguous owners stay unresolved.
func (s *Store) ReportActivitySubjects(ctx context.Context, refs []string, visible func(string, string) bool) ([]RefPreview, error) {
	if len(refs) > 200 {
		return nil, invalidBoardRequest("at most 200 activity subjects")
	}
	normalized := append([]string(nil), refs...)
	threads := []string{}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "thread:") {
			threads = append(threads, strings.TrimPrefix(ref, "thread:"))
		}
	}
	if len(threads) > 0 {
		raw, _ := json.Marshal(threads)
		rows, err := s.db.QueryContext(ctx, `SELECT requested.value,c.id,COALESCE(NULLIF(c.handle,''),c.id) FROM json_each(?) requested JOIN cards c ON c.id IN (SELECT owner.id FROM cards owner WHERE owner.thread_id=requested.value LIMIT 2)`, string(raw))
		if err != nil {
			return nil, err
		}
		owners := map[string][]string{}
		for rows.Next() {
			var thread, id, handle string
			if err = rows.Scan(&thread, &id, &handle); err != nil {
				rows.Close()
				return nil, err
			}
			owners[thread] = append(owners[thread], "card:"+handle)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for i, ref := range refs {
			if values := owners[strings.TrimPrefix(ref, "thread:")]; strings.HasPrefix(ref, "thread:") && len(values) == 1 {
				normalized[i] = values[0]
			}
		}
	}
	facts, err := s.readRefProjection(ctx, normalized, visible, true)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, fact := range facts {
		if fact.Resolvable && fact.Kind == "card" {
			ids = append(ids, fact.ID)
		}
	}
	if len(ids) > 0 {
		raw, _ := json.Marshal(ids)
		rows, err := s.db.QueryContext(ctx, `SELECT c.id,COALESCE(NULLIF(c.handle,''),c.id),`+workIsInitiativeSQL("c")+` FROM cards c WHERE c.id IN (SELECT value FROM json_each(?))`, string(raw))
		if err != nil {
			return nil, err
		}
		type item struct{ ref, kind string }
		metadata := map[string]item{}
		for rows.Next() {
			var id, handle string
			var initiative bool
			if err = rows.Scan(&id, &handle, &initiative); err != nil {
				rows.Close()
				return nil, err
			}
			kind := "card"
			if initiative {
				kind = "initiative"
			}
			metadata[id] = item{"card:" + handle, kind}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for i, fact := range facts {
			if m, ok := metadata[fact.ID]; ok {
				facts[i].Ref = m.ref
				facts[i].Kind = m.kind
			}
		}
	}
	return facts, nil
}
