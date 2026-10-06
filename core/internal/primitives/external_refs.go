package primitives

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// MarshalJSON keeps native status optional and emits explicit null for a
// parsed source link, whose existence does not establish workflow status.
func (p RefPreview) MarshalJSON() ([]byte, error) {
	type plain RefPreview
	if p.Kind == "external" {
		var status, link *string
		if p.Status != "" {
			status = &p.Status
		}
		if p.URL != "" {
			link = &p.URL
		}
		return json.Marshal(struct {
			plain
			Status *string `json:"status"`
			URL    *string `json:"url"`
		}{plain: plain(p), Status: status, URL: link})
	}
	return json.Marshal(plain(p))
}

type externalEvidence struct {
	Authority    string `json:"authority"`
	ConnectionID string `json:"connection_id"`
	NativeID     string `json:"native_id"`
	Identifier   string `json:"identifier"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Status       string `json:"status"`
	Phase        string `json:"phase"`
	ObservedAt   string `json:"observed_at"`
	ActivityAt   string `json:"source_activity_at"`
}

// readExternalRefFacts looks up only requested identities in the evidence index,
// applying SQL access before candidate limits and decoding evidence JSON.
func (s *Store) readExternalRefFacts(ctx context.Context, refs []string, out []RefPreview, visible func(string, string) bool) error {
	keys := map[string][]int{}
	publicKeys := map[string][]int{}
	for i, ref := range refs {
		kind, _, _ := strings.Cut(ref, ":")
		if kind == "card" || kind == "doc" || kind == "document" || kind == "board" || kind == "topic" {
			continue
		}
		keys[ref] = append(keys[ref], i)
		lookup := []string{}
		a, id, link, ok := plans.ParseExternalRef(ref)
		if ok {
			out[i] = RefPreview{Ref: ref, Kind: "external", Authority: a, NativeID: id, URL: link, Source: "parsed", Resolvable: true}
			// GitHub's public link forms share one parsed native identity. Index keys
			// themselves remain generic exact strings published by any adapter.
			base, number, _ := strings.Cut(id, "#")
			lookup = append(lookup, id, a+":"+id, base+"/issues/"+number, base+"/pull/"+number, "https://github.com/"+base+"/issues/"+number, "https://github.com/"+base+"/pull/"+number)
		}
		for _, key := range lookup {
			publicKeys[strings.ToLower(key)] = append(publicKeys[strings.ToLower(key)], i)
		}
	}
	lookup := []string{}
	for key := range keys {
		lookup = append(lookup, key)
	}
	if len(lookup) == 0 {
		return nil
	}
	publicLookup := []string{}
	for key := range publicKeys {
		publicLookup = append(publicLookup, key)
	}
	publicEncoded, err := json.Marshal(publicLookup)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(lookup)
	if err != nil {
		return err
	}
	matches := make([]map[string]externalEvidence, len(refs))
	counts := make([]int, len(refs))
	seenCandidates := make([]map[int64]bool, len(refs))
	var cursor int64
	for {
		rows, err := s.db.QueryContext(ctx, `WITH candidates AS (
 SELECT CAST(bounded.value AS INTEGER) AS id FROM json_each(?) requested CROSS JOIN json_each((SELECT json_group_array(id) FROM (SELECT limited.id FROM work_evidence_index limited INDEXED BY idx_work_evidence_lookup JOIN cards access_card ON access_card.id=limited.card_id WHERE limited.lookup_key=requested.value AND access_card.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM boards lifecycle_board WHERE lifecycle_board.id=access_card.board_id AND lifecycle_board.trashed_at IS NOT NULL) ORDER BY limited.id LIMIT 33))) bounded WHERE CAST(bounded.value AS INTEGER)>?
 UNION SELECT CAST(bounded.value AS INTEGER) AS id FROM json_each(?) requested CROSS JOIN json_each((SELECT json_group_array(id) FROM (SELECT limited.id FROM work_evidence_index limited INDEXED BY idx_work_evidence_public_lookup JOIN cards access_card ON access_card.id=limited.card_id WHERE limited.lookup_key COLLATE NOCASE=requested.value AND access_card.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM boards lifecycle_board WHERE lifecycle_board.id=access_card.board_id AND lifecycle_board.trashed_at IS NOT NULL) ORDER BY limited.id LIMIT 33))) bounded WHERE CAST(bounded.value AS INTEGER)>?
 ORDER BY 1 LIMIT 200)
 SELECT e.id,e.evidence_id,e.lookup_key,record.evidence_json,c.updated_at,
 COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id),''),COALESCE(json_extract(t.body_json,'$.pm_actor_id'),''),COALESCE(b.thread_id,''),COALESCE(json_extract(bt.body_json,'$.pm_actor_id'),''),COALESCE(c.trashed_at,''),COALESCE(b.trashed_at,'')
 FROM candidates candidate JOIN work_evidence_index e ON e.id=candidate.id JOIN cards c ON c.id=e.card_id JOIN work_evidence_records record ON record.id=e.evidence_id
 LEFT JOIN threads t ON t.id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id))
 LEFT JOIN boards b ON b.id=c.board_id LEFT JOIN threads bt ON bt.id=b.thread_id
 ORDER BY e.id`, string(encoded), cursor, string(publicEncoded), cursor)
		if err != nil {
			return err
		}
		type candidate struct {
			id, evidenceID                                                                  int64
			key, raw, at, thread, owner, boardThread, boardOwner, cardTrashed, boardTrashed string
		}
		batch := []candidate{}
		for rows.Next() {
			var c candidate
			if err = rows.Scan(&c.id, &c.evidenceID, &c.key, &c.raw, &c.at, &c.thread, &c.owner, &c.boardThread, &c.boardOwner, &c.cardTrashed, &c.boardTrashed); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, c := range batch {
			cursor = c.id
			indices := append([]int{}, keys[c.key]...)
			indices = append(indices, publicKeys[strings.ToLower(c.key)]...)
			if c.cardTrashed != "" || c.boardTrashed != "" {
				continue
			}
			if visible != nil && (!visible(c.thread, c.owner) || !visible(c.boardThread, c.boardOwner)) {
				continue
			}
			unique := map[int]bool{}
			for _, i := range indices {
				unique[i] = true
			}
			indices = nil
			for i := range unique {
				if counts[i] > 32 {
					continue
				}
				if seenCandidates[i] == nil {
					seenCandidates[i] = map[int64]bool{}
				}
				if seenCandidates[i][c.evidenceID] {
					continue
				}
				seenCandidates[i][c.evidenceID] = true
				counts[i]++
				if counts[i] <= 32 {
					indices = append(indices, i)
				}
			}
			needed := false
			for _, i := range indices {
				if len(matches[i]) < 2 {
					needed = true
				}
			}
			if !needed || len(indices) == 0 {
				continue
			}
			var e externalEvidence
			if json.Unmarshal([]byte(c.raw), &e) != nil || e.Authority == "" || e.ConnectionID == "" || e.NativeID == "" {
				continue
			}
			if e.URL != "" {
				u, err := url.Parse(e.URL)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
					continue
				}
			}
			if e.ObservedAt == "" {
				e.ObservedAt = c.at
			}
			for _, i := range indices {
				if out[i].Resolvable && out[i].Kind != "external" {
					continue
				}
				if a, id, _, ok := plans.ParseExternalRef(refs[i]); ok {
					// A conflicting public link cannot override the parsed identity.
					normalize := func(v string) string {
						aa, nn, _, yes := plans.ParseExternalRef(v)
						if !yes {
							aa, nn, _, yes = plans.ParseExternalRef("https://github.com/" + v)
						}
						if yes && aa == a {
							return strings.ToLower(nn)
						}
						return ""
					}
					n, u := normalize(e.NativeID), normalize(e.URL)
					if e.Authority != a || (n != "" && n != strings.ToLower(id)) || (u != "" && u != strings.ToLower(id)) {
						continue
					}
				}
				identity := e.Authority + "\x00" + e.ConnectionID + "\x00" + e.NativeID
				if matches[i] == nil {
					matches[i] = map[string]externalEvidence{}
				}
				old, exists := matches[i][identity]
				// Two identities already prove ambiguity; bound retained candidate state.
				if !exists && len(matches[i]) >= 2 {
					continue
				}
				oldAt, _ := time.Parse(time.RFC3339Nano, old.ObservedAt)
				newAt, _ := time.Parse(time.RFC3339Nano, e.ObservedAt)
				if !exists || newAt.After(oldAt) {
					matches[i][identity] = e
				}
			}
		}
		if len(batch) < 200 {
			break
		}
	}
	for i, found := range matches {
		if len(found) != 1 || counts[i] > 32 {
			continue
		}
		for _, e := range found {
			p := &out[i]
			p.Resolvable = true
			p.Kind = "external"
			p.Authority = e.Authority
			p.NativeID = e.NativeID
			p.ConnectionID = e.ConnectionID
			p.Title = e.Title
			p.Status = e.Status
			p.Phase = firstNonEmptyString(e.Phase, e.Status)
			p.Source = "evidence"
			p.ObservedAt = e.ObservedAt
			if e.URL != "" {
				p.URL = e.URL
			}
			p.MovementAt, _ = time.Parse(time.RFC3339Nano, e.ActivityAt)
			p.LastMovedAt = e.ActivityAt
		}
	}
	return nil
}
