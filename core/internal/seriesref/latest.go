package seriesref

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// LatestRefs is the brief's bounded metadata/last-value read. It shares the
// CompareRefs label-admission semantics but does not fetch baseline points,
// adapter freshness or comparisons which brief rows never serialize.
func LatestRefs(ctx context.Context, reader RefReader, refs []Ref, now time.Time) (map[string]Metric, error) {
	out := map[string]Metric{}
	if len(refs) == 0 {
		return out, nil
	}
	if len(refs) > 100 {
		return nil, ErrCapacity
	}
	for i := range refs {
		refs[i].Since = now.Add(-refs[i].Range).UnixNano()
	}
	encoded, _ := json.Marshal(refs)
	rows, err := reader.QueryContext(ctx, `WITH requested AS MATERIALIZED (
 SELECT json_extract(value,'$.ref') ref,json_extract(value,'$.name') name,json_extract(value,'$.labels') selectors,json_extract(value,'$.since') since FROM json_each(?)
 ), selected AS MATERIALIZED (
 SELECT r.ref,r.name,r.since,d.kind,labels.value labels
 FROM requested r JOIN series_definitions d ON d.name=r.name JOIN series_adapters a ON a.name=d.adapter
 LEFT JOIN json_each(CASE WHEN r.selectors='{}' THEN
 (SELECT json_group_array(labels) FROM (SELECT labels FROM series_labels WHERE series=r.name ORDER BY labels LIMIT 2))
 ELSE (SELECT json_group_array(labels) FROM (
 SELECT labels FROM (SELECT labels FROM series_labels WHERE series=r.name ORDER BY labels LIMIT 100) l
 WHERE NOT EXISTS (SELECT 1 FROM json_each(r.selectors) filter WHERE json_extract(l.labels,'$."'||filter.key||'"') IS NOT filter.value)
 ORDER BY labels LIMIT 2)) END) labels ON 1=1
 ) SELECT ref,name,kind,labels,
 (SELECT json_object('ts',ts,'value',value,'state',state) FROM series_points WHERE series=selected.name AND labels=selected.labels AND ts>=selected.since AND ts<=? ORDER BY ts DESC LIMIT 1)
 FROM selected`, string(encoded), now.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Metric
		var labels, last sql.NullString
		if err = rows.Scan(&m.Ref, &m.Name, &m.Kind, &labels, &last); err != nil {
			return nil, err
		}
		if _, ok := out[m.Ref]; ok {
			out[m.Ref] = Metric{Ref: m.Ref, Definition: m.Definition, Status: "ambiguous"}
			continue
		}
		m.Status = "unavailable"
		if labels.Valid {
			if err = json.Unmarshal([]byte(labels.String), &m.Labels); err != nil {
				return nil, err
			}
		}
		if last.Valid {
			var p struct {
				TS    int64    `json:"ts"`
				Value *float64 `json:"value"`
				State *string  `json:"state"`
			}
			if err = json.Unmarshal([]byte(last.String), &p); err != nil {
				return nil, err
			}
			m.Latest = &Observation{TS: time.Unix(0, p.TS).UTC().Format(time.RFC3339Nano), Value: p.Value, State: p.State}
			m.Status = "ok"
		}
		out[m.Ref] = m
	}
	return out, rows.Err()
}
