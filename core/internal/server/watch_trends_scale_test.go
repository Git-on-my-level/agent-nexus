package server

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

var watchTrendPrivacyMechanisms = []string{"board", "subject", "pm-thread", "blocks", "payload-ref", "refs", "prose", "metadata", "evidence", "card-ref", "card-prose", "unconditional", "tombstone", "conflicting", "parent-thread", "revision"}

// Each mechanism contributes at least 2,000 of every trend position family,
// including open/closed promises, open/answered asks and Done lifecycle events.
// These are canonical writes: the ownership and position triggers stay enabled.
func seedWatchTrendsInheritedScale(t *testing.T, db *sql.DB, board, privateBoard, thread, privateRef, publicRef string, mechanisms ...string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	const n = `WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<4000) `
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	exec(`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json) VALUES('scale-unknown','text',?,'owner','text/plain','unknown',NULL)`, stamp)
	exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('scale-other-owner',?,'owner','{"pm_actor_id":"other-owner"}')`, stamp)
	exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','scale-retained','card:scale-retained','owner')`)
	exec(`INSERT INTO artifacts(id,kind,thread_id,created_at,created_by,content_type,content_hash,content_refs_json) SELECT 'scale-private-artifact','text',thread_id,?,'owner','text/plain','private','[]' FROM boards WHERE id=?`, stamp, privateBoard)
	for _, mechanism := range mechanisms {
		t.Logf("seed 2,000 per trend family: %s", mechanism)
		prefix := "scale-" + mechanism + "-"
		// A shared backing thread keeps fixture setup small. Each family still
		// has 2,000+ distinct records; every source inherits its canonical policy.
		body := `'{}'`
		if mechanism == "pm-thread" {
			body = `json_object('pm_actor_id','owner')`
		}
		exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,?,'owner',`+body+`)`, prefix+"thread", stamp)
		cardBoard := board
		if mechanism == "board" {
			cardBoard = privateBoard
		}
		refs, summary := `'[]'`, `''`
		if mechanism == "card-ref" {
			refs = `json_array(@private)`
		}
		if mechanism == "card-prose" {
			summary = `'See ('||@private||') for details'`
		}
		exec(n+`INSERT INTO cards(id,board_id,title,summary,column_key,thread_id,refs_json,created_at,created_by,updated_at,updated_by)
   SELECT @prefix||i,@board,'Hidden',`+summary+`,CASE WHEN i<2000 THEN 'todo' ELSE 'done' END,@prefix||'thread',`+refs+`,@stamp,'owner',@stamp,'owner' FROM n`, sql.Named("prefix", prefix), sql.Named("board", cardBoard), sql.Named("stamp", stamp), sql.Named("private", privateRef))
		if mechanism == "parent-thread" {
			exec(`UPDATE cards SET parent_thread_id=(SELECT thread_id FROM boards WHERE id=?) WHERE id>=? AND id<?`, privateBoard, prefix, prefix+"~")
		}
		if mechanism == "revision" {
			exec(n+`INSERT INTO card_revisions(revision_id,card_id,revision_number,artifact_id,created_at,created_by) SELECT @prefix||'rev-'||i,@prefix||i,1,'scale-private-artifact',@stamp,'owner' FROM n`, sql.Named("prefix", prefix), sql.Named("stamp", stamp))
		}
		meta := `json_object('cadence',json_object('every_seconds',3600))`
		switch mechanism {
		case "unconditional":
			meta = `json_object('cadence',json_object('every_seconds',3600),'related_refs',json_array('artifact:scale-unknown'))`
		case "tombstone":
			meta = `json_object('cadence',json_object('every_seconds',3600),'related_refs',json_array('card:scale-retained'))`
		case "conflicting":
			meta = `json_object('cadence',json_object('every_seconds',3600),'related_refs',json_array(@private,'thread:scale-other-owner'))`
		case "metadata":
			meta = `json_object('cadence',json_object('every_seconds',3600),'related_refs',json_array(@private))`
		case "evidence":
			meta = `json_object('cadence',json_object('every_seconds',3600),'evidence',json_array(json_object('ref',@private)))`
		// Event mechanisms use a public card host: their privacy is not inherited
		// from their own container. Hide the bulk cards separately with that same
		// reference to keep the fixture's visible population unchanged.
		case "subject", "blocks", "payload-ref", "refs", "prose":
			meta = `json_object('cadence',json_object('every_seconds',3600),'related_refs',json_array(@private))`
		}
		exec(n+`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) SELECT @prefix||i,`+meta+`,@stamp,'owner' FROM n`, sql.Named("prefix", prefix), sql.Named("stamp", stamp), sql.Named("private", privateRef))
		exec(n+`INSERT INTO brief_initiatives(card_id,open,promise_at,loop_annotated,loop_annotated_at) SELECT @prefix||i,CASE WHEN i<2000 THEN 1 ELSE 0 END,@stamp,1,@stamp FROM n WHERE 1 ON CONFLICT(card_id) DO UPDATE SET open=excluded.open,promise_at=excluded.promise_at,loop_annotated=excluded.loop_annotated,loop_annotated_at=excluded.loop_annotated_at`, sql.Named("prefix", prefix), sql.Named("stamp", stamp))
		eventThread, eventSubject, eventCard := `@prefix||'thread'`, `'card:'||@prefix||i`, `@prefix||i`
		if mechanism == "subject" || mechanism == "blocks" || mechanism == "payload-ref" || mechanism == "refs" || mechanism == "prose" {
			eventThread, eventSubject, eventCard = `@thread`, `@subject`, `substr(@subject,6)`
		}
		if mechanism == "subject" {
			eventSubject = `@private`
		}
		for _, family := range []string{"ask", "answer", "run", "done"} {
			eventType := map[string]string{"ask": "human_attention_requested", "answer": "human_attention_responded", "run": "run_recorded", "done": "card_moved"}[family]
			payload := `json_object('kind','ask','title','Q','requester_actor_id','agent-hidden','subject_ref',` + eventSubject + `,'response_proposals',json_array('Proceed'),'run_id',i,'outcome','answered','card_id',` + eventCard + `,'column_key','done','from_column_key','todo')`
			switch mechanism {
			case "blocks":
				payload = `json_set(` + payload + `,'$.blocks',json_array(@private))`
			case "payload-ref":
				payload = `json_set(` + payload + `,'$.other_ref',@private)`
			case "prose":
				payload = `json_set(` + payload + `,'$.body','See ('||@private||') for details')`
			}
			eventRefs := `json_array('thread:'||` + eventThread + `)`
			if mechanism == "refs" {
				eventRefs = `json_array(@private)`
			}
			count := 4000
			if family != "ask" {
				count = 2000
			}
			exec(n+`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT @prefix||@family||i,@type,@stamp,'agent-hidden',`+eventThread+`,`+eventRefs+`,json_object('payload',`+payload+`) FROM n WHERE i<@count`, sql.Named("prefix", prefix), sql.Named("family", family), sql.Named("type", eventType), sql.Named("stamp", stamp), sql.Named("thread", thread), sql.Named("subject", publicRef), sql.Named("private", privateRef), sql.Named("count", count))
		}
		exec(n+`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) SELECT @prefix||'ask'||i,@prefix||'answer'||i,'answered','owner',@stamp FROM n WHERE i<2000`, sql.Named("prefix", prefix), sql.Named("stamp", stamp))
		// Check the routing invariant as well as measuring it. All fixture rows
		// must leave the public partition, for every kind (not only raw events).
		for kind, min := range map[string]int{"ask": 4000, "answer": 2000, "run": 2000, "open_ask": 4000, "card": 4000, "card_done": 2000, "promise_open": 2000, "promise_closed": 2000, "loop": 4000, "done_event": 2000} {
			var total, public int
			if err := tx.QueryRow(`SELECT count(*),COALESCE(sum(scope=''),0) FROM watch_trend_positions WHERE kind=? AND id>=? AND id<?`, kind, prefix, prefix+"~").Scan(&total, &public); err != nil {
				t.Fatal(err)
			}
			if total < min || public != 0 {
				t.Fatalf("%s %s: total=%d (want >=%d), public=%d", mechanism, kind, total, min, public)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(fmt.Errorf("seed inherited privacy: %w", err))
	}
}
