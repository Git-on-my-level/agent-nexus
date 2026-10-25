package storage

import (
	"context"
	"database/sql"
)

// This upgrade is a one-time historical sweep, not a serving read. It also
// catches asks whose original proposal-ledger write failed. Resolved requests
// are untouched. Deterministic event ids make preview upgrades idempotent.
func quietCurationSuggestions(ctx context.Context, tx *sql.Tx) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_curation_proposals_open_page ON curation_proposals(status,created_at,key)`,
		`INSERT OR IGNORE INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json)
 SELECT 'curation-withdraw:'||e.id,'human_attention_withdrawn',strftime('%Y-%m-%dT%H:%M:%fZ','now'),'system',e.thread_id,
 CASE WHEN COALESCE(e.thread_id,'')='' THEN json_array('event:'||e.id) ELSE json_array('event:'||e.id,'thread:'||e.thread_id) END,
 json_object('summary','Curation suggestion moved out of human attention','provenance',json_object('sources',json_array('event:'||e.id)),
 'payload',json_object('request_event_id',e.id,'request_event_ref','event:'||e.id,
 'requester_actor_id',COALESCE(json_extract(e.payload_json,'$.payload.requester_actor_id'),''),
 'withdrawn_by_actor_id','system','reason','Curation is agent work; this suggestion must not interrupt a human.'))
 FROM events e WHERE e.type='human_attention_requested'
 AND json_type(e.payload_json,'$.payload.curation')='object'
 AND NOT EXISTS(SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=e.id)
 AND NOT EXISTS(SELECT 1 FROM access_requests a WHERE a.request_event_id=e.id)`,
		`INSERT OR IGNORE INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at)
 SELECT 'curation-withdraw-edge:'||e.id||':'||r.key,'event',e.id,substr(r.value,1,instr(r.value,':')-1),substr(r.value,instr(r.value,':')+1),'ref',e.ts
 FROM events e,json_each(e.refs_json) r WHERE e.type='human_attention_withdrawn' AND e.id='curation-withdraw:'||json_extract(e.payload_json,'$.payload.request_event_id')`,
		`INSERT OR IGNORE INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at)
 SELECT json_extract(e.payload_json,'$.payload.request_event_id'),e.id,'withdrawn','system',e.ts
 FROM events e WHERE e.type='human_attention_withdrawn' AND e.id='curation-withdraw:'||json_extract(e.payload_json,'$.payload.request_event_id')`,
		// Resolution claims alone do not refresh materialized inbox rows.
		// Remove exact legacy sources immediately; their thread summaries are
		// invalidated in the same commit for the ordinary projection worker.
		`DELETE FROM derived_inbox_items WHERE source_event_id IN (
 SELECT json_extract(e.payload_json,'$.payload.request_event_id') FROM events e
 WHERE e.type='human_attention_withdrawn' AND e.id='curation-withdraw:'||json_extract(e.payload_json,'$.payload.request_event_id'))`,
		`INSERT INTO derived_topic_dirty_queue(thread_id,dirty_at)
 SELECT DISTINCT e.thread_id,strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM events e
 WHERE e.type='human_attention_withdrawn' AND e.id='curation-withdraw:'||json_extract(e.payload_json,'$.payload.request_event_id') AND COALESCE(e.thread_id,'')<>''
 ON CONFLICT(thread_id) DO UPDATE SET dirty_at=MIN(derived_topic_dirty_queue.dirty_at,excluded.dirty_at)`,
		`INSERT INTO topic_projection_refresh_status(thread_id,desired_generation,materialized_generation,queued_at,updated_at)
 SELECT DISTINCT e.thread_id,1,0,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM events e
 WHERE e.type='human_attention_withdrawn' AND e.id='curation-withdraw:'||json_extract(e.payload_json,'$.payload.request_event_id') AND COALESCE(e.thread_id,'')<>''
 ON CONFLICT(thread_id) DO UPDATE SET desired_generation=topic_projection_refresh_status.desired_generation+1,
 queued_at=CASE WHEN topic_projection_refresh_status.queued_at IS NULL OR topic_projection_refresh_status.queued_at>excluded.queued_at THEN excluded.queued_at ELSE topic_projection_refresh_status.queued_at END,updated_at=excluded.updated_at`,
		`UPDATE curation_proposals SET status='dismissed',resolved_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE status='open' AND proposal_event_id IN (SELECT e.id FROM events e WHERE e.type='human_attention_requested' AND json_type(e.payload_json,'$.payload.curation')='object')`,
	}
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
