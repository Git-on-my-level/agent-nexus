package primitives

import (
	"context"
	"encoding/json"
	"strings"

	"agent-nexus-visualreport"
)

// Classify only names referenced by this bounded projection. Correlated EXISTS
// selectors use identity indexes; no actor/principal directory is materialized.
func (s *Store) overviewPeople(ctx context.Context, work []map[string]any, humans, agents map[string]bool) (map[string]bool, map[string]bool, error) {
	if humans == nil {
		humans = map[string]bool{}
	}
	if agents == nil {
		agents = map[string]bool{}
	}
	ids, names := []string{}, []string{}
	for _, w := range work {
		if next := strings.TrimPrefix(strings.TrimSpace(anyStringValue(w["next_actor"])), "actor:"); next != "" {
			ids = append(ids, next)
		}
		_, _, needs := visualreport.Summary(anyStringValue(w["summary"]))
		for _, line := range needs {
			names = append(names, strings.ToLower(strings.TrimSpace(strings.SplitN(line[len("Needs"):], ":", 2)[0])))
		}
	}
	if len(ids) == 0 && len(names) == 0 {
		return humans, agents, nil
	}
	ids = uniqueSortedStrings(ids)
	names = uniqueSortedStrings(names)
	idJSON, _ := json.Marshal(ids)
	nameJSON, _ := json.Marshal(names)
	kind := `COALESCE(NULLIF(json_extract(p.metadata_json,'$.principal_kind'),''),CASE WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id=p.id) THEN 'human' ELSE 'agent' END)`
	rows, err := s.db.QueryContext(ctx, `SELECT j.value,
 EXISTS(SELECT 1 FROM actors x,json_each(x.tags_json) tag WHERE x.id=j.value AND lower(tag.value)='human')
 OR EXISTS(SELECT 1 FROM agents p WHERE p.actor_id=j.value AND `+kind+`='human')
 FROM json_each(?) j WHERE j.value IS NOT NULL`, string(idJSON))
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id string
		var human bool
		if err = rows.Scan(&id, &human); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if human {
			humans[id] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT j.value,
 EXISTS(SELECT 1 FROM agents p WHERE anx_unicode_lower(p.username)=j.value AND `+kind+`='agent')
 OR EXISTS(SELECT 1 FROM actors x JOIN agents p ON p.actor_id=x.id WHERE anx_unicode_lower(x.display_name)=j.value AND `+kind+`='agent')
 FROM json_each(?) j WHERE j.value IS NOT NULL`, string(nameJSON))
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var name string
		var agent bool
		if err = rows.Scan(&name, &agent); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if agent {
			agents[name] = true
		}
	}
	err = rows.Err()
	rows.Close()
	return humans, agents, err
}
