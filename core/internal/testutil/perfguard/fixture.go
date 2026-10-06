// Package perfguard contains synthetic data and SQL instrumentation for scale
// integration tests. It never reads a real workspace or production credentials.
package perfguard

import (
	"agent-nexus-core/internal/pm"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const Rows = 4096
const Timestamp = "2026-01-01T00:00:00Z"

// Seed builds one deterministic corpus in a transaction. Call once, then reuse
// the migrated database across route samples. Canonical triggers stay enabled:
// the fixture must exercise the real ownership graph and reference indexes.
func Seed(ctx context.Context, db *sql.DB, contentDir, owner string, seriesAnchor time.Time) error {
	if _, err := pm.NewStore(db); err != nil {
		return err
	}
	body := strings.Repeat("Synthetic scale corpus searchable evidence. ", 384)
	hash := sha256.Sum256([]byte(body))
	blobHash := hex.EncodeToString(hash[:])
	if err := os.WriteFile(filepath.Join(contentDir, blobHash), []byte(body), 0o600); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,'Scale owner','["human"]',?,'{}') ON CONFLICT(id) DO NOTHING`, []any{owner, Timestamp}},
		{`CREATE TEMP TABLE scale_seq(n INTEGER PRIMARY KEY)`, nil},
		{`INSERT INTO scale_seq WITH RECURSIVE s(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM s WHERE n+1 < ?) SELECT n FROM s`, []any{Rows}},
		{`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) SELECT 'scale-principal-'||n,'Scale principal '||n,'["agent"]',?,'{}' FROM scale_seq`, []any{Timestamp}},
		{`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT 'scale-agent-'||n,'scale.agent.'||n,'scale-principal-'||n,?,?,'{}' FROM scale_seq`, []any{Timestamp, Timestamp}},
		{`INSERT INTO threads(id,updated_at,updated_by,body_json) SELECT 'scale-board-thread-'||n,?,?,CASE WHEN n=0 THEN '{}' ELSE json_object('pm_actor_id',?) END FROM scale_seq WHERE n<9`, []any{Timestamp, owner, owner}},
		{`INSERT INTO boards(id,handle,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) SELECT 'scale-board-'||n,'scale-board-'||n,'Scale board '||n,'scale-board-thread-'||n,'[{"key":"ready","title":"Ready"}]',?,?,?,? FROM scale_seq WHERE n<9`, []any{Timestamp, owner, Timestamp, owner}},
		{`INSERT INTO threads(id,updated_at,updated_by,body_json) SELECT 'scale-thread-'||n,?,?,'{}' FROM scale_seq`, []any{Timestamp, owner}},
		{`INSERT INTO cards(id,handle,board_id,thread_id,title,summary,column_key,head_revision_id,created_at,created_by,updated_at,updated_by) SELECT 'scale-card-'||n,'scale-card-'||n,'scale-board-'||(n%9),'scale-thread-'||n,'Scale card '||n,?,'ready','scale-card-revision-'||n,?,?,?,? FROM scale_seq`, []any{body, Timestamp, owner, Timestamp, owner}},
		{`INSERT INTO artifacts(id,handle,kind,thread_id,created_at,created_by,content_type,content_hash,metadata_json,content_refs_json) SELECT 'scale-artifact-'||n,'scale-artifact-'||n,'note','scale-thread-'||n,?,?,'text/plain',?,json_object('description',?),'[]' FROM scale_seq`, []any{Timestamp, owner, blobHash, body}},
		{`INSERT INTO documents(id,handle,thread_id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) SELECT 'scale-doc-'||n,'scale-doc-'||n,'scale-thread-'||n,'Scale document '||n,'scale-doc-revision-'||n,1,?,?,?,? FROM scale_seq`, []any{Timestamp, owner, Timestamp, owner}},
		{`INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,thread_id,created_at,created_by) SELECT 'scale-doc-revision-'||n,'scale-doc-'||n,1,'scale-artifact-'||n,'scale-thread-'||n,?,? FROM scale_seq`, []any{Timestamp, owner}},
		{`INSERT INTO card_revisions(revision_id,card_id,revision_number,artifact_id,thread_id,created_at,created_by) SELECT 'scale-card-revision-'||n,'scale-card-'||n,1,'scale-artifact-'||n,'scale-thread-'||n,?,? FROM scale_seq`, []any{Timestamp, owner}},
		{`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT 'scale-event-'||n,'scale-event-'||n,'message_posted',?,?,'scale-thread-'||n,json_array('card:scale-card-'||n),json_object('text',?) FROM scale_seq`, []any{Timestamp, owner, body}},
		{`INSERT INTO card_plans(card_id,body_json,updated_at) SELECT 'scale-card-'||n,json_object('steps',json_array(json_object('id','deliver','title','Deliver synthetic evidence','ref','doc:scale-doc-'||n))),? FROM scale_seq`, []any{Timestamp}},
		{`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,source_event_id,source_card_id,generated_at,data_json) SELECT 'scale-inbox-'||n,'scale-thread-'||n,'decision_request',?,'scale-event-'||n,'scale-card-'||n,?,json_object('id','scale-inbox-'||n,'kind','decision_request','status','open','thread_id','scale-thread-'||n,'title','Scale inbox item','related_refs',json_array('card:scale-card-'||n)) FROM scale_seq`, []any{Timestamp, Timestamp}},
		{`INSERT INTO topics(id,handle,title,thread_id,created_at,created_by,updated_at,updated_by) SELECT 'scale-topic-'||n,'scale-topic-'||n,'Scale topic '||n,'scale-thread-'||n,?,?,?,? FROM scale_seq`, []any{Timestamp, owner, Timestamp, owner}},
		{`INSERT INTO document_fts(document_id,title,body,summary,source,tags,comments) SELECT 'scale-doc-'||n,'Scale document '||n,?,'','','','' FROM scale_seq`, []any{body}},
		{`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) SELECT 'scale-board-card-edge-'||n,'board','scale-board-'||(n%9),'card','scale-card-'||n,'board_card',?,json_object('column_key','ready','rank',printf('%06d',n)) FROM scale_seq`, []any{Timestamp}},
		{`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('scale-host','scale-host','Synthetic host','test','test','[]',?)`, []any{Timestamp}},
		{`INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at) VALUES('scale-adapter','Synthetic adapter','scale-agent-0','scale-host',60,?)`, []any{Timestamp}},
		{`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES('scale.series','scale-adapter','count','gauge')`, nil},
		{`INSERT INTO series_labels(series,labels) VALUES('scale.series','{}')`, nil},
		{`INSERT INTO series_points(series,labels,ts,value,received_day) SELECT 'scale.series','{}',?+n*1000000000,n,? FROM scale_seq`, []any{seriesAnchor.Add(-2 * time.Hour).UnixNano(), seriesAnchor.Unix() / 86400}},
		{`INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) SELECT kinds.kind,'scale-pm-'||kinds.kind||'-'||n,'ws_main',?, 'scale-pm-conversation-'||n,1,json_object('id','scale-pm-'||kinds.kind||'-'||n,'workspace_id','ws_main','actor_id',?,'conversation_id','scale-pm-conversation-'||n,'decision_id','scale-pm-decision-'||n,'work_ref','card:scale-card-'||n,'title','Scale PM record','text','Synthetic PM context','status','pending','revision',1,'created_at',?) FROM scale_seq CROSS JOIN (SELECT 'conversation' kind UNION ALL SELECT 'decision' UNION ALL SELECT 'action' UNION ALL SELECT 'turn') kinds`, []any{owner, owner, Timestamp}},
		{`INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,state,liveness,result_collected,labels_json,card_ref,last_observed_at) SELECT 'scale-run-'||n,'scale-run-'||n,'test','synthetic-'||n,'scale-host','scale-agent-'||n,'test','running','alive',0,'[]','card:scale-card-'||n,? FROM scale_seq`, []any{Timestamp}},
		{`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) SELECT 'scale-host','principal-'||n,'scale-agent-'||n,'agent' FROM scale_seq`, nil},
		{`DROP TABLE scale_seq`, nil},
	}
	for i, s := range statements {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return fmt.Errorf("scale fixture statement %d: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "ANALYZE")
	return err
}
