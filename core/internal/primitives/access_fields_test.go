package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

func TestResourceAccessScopeRechecksRootsAndQuotedRelations(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "public"})
	if err != nil {
		t.Fatal(err)
	}
	scope := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if err = s.CheckResourceValues(scope, map[string]any{"ref": card["ref"]}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(card["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckResourceValues(scope, map[string]any{"ref": card["ref"]}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty-root decision was cached: %v", err)
	}
	for _, table := range []string{`"CARDS"`, "`cards`", `[cards]`} {
		var count int
		if err = resourceaccess.NewDB(ws.DB()).QueryRowContext(scope, `SELECT COUNT(*) FROM `+table+` WHERE id=?`, card["id"]).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("quoted relation bypass: %s", table)
		}
	}
	for _, query := range []string{`SELECT 1`, `WITH value(n) AS (SELECT 1) SELECT n FROM value`, `SELECT EXISTS(SELECT 1 FROM _anx_denied)`} {
		var value int
		if err = s.db.QueryRowContext(scope, query).Scan(&value); err != nil || value != 1 {
			t.Fatalf("independent/internal query %q: value=%d err=%v", query, value, err)
		}
	}
	var version int
	if err = s.db.QueryRowContext(scope, `PRAGMA user_version`).Scan(&version); err == nil {
		t.Fatal("unclassified query form bypassed scoped-read validation")
	}
}

func TestResourceAccessEveryOwnershipField(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "field secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "field public"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(public["id"])
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "field document"}, "public content", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "field topic", "summary": "public", "related_refs": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "public"}})
	if err != nil {
		t.Fatal(err)
	}
	obs, err := s.SubmitWorkObservation(ctx, "owner", id, map[string]any{"idempotency_key": "fixture", "reader_id": "fixture", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpsertAgentWakeup(ctx, AgentWakeup{WakeupID: "field-wakeup", TargetActorID: "owner", Status: AgentWakeupStatusRequested}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES('` + id + `','{}','now')`,
		`INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES('field-participant','` + id + `','fixture','active',1,'{}','now','now','later')`,
		`INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,state,liveness,result_collected,labels_json,last_observed_at) VALUES('field-run','field-run','fixture','fixture','fixture','owner','fixture','running','active',0,'[]','now')`,
	} {
		if _, err = ws.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	fixtures := map[string]string{"threads": anyStringValue(public["thread_id"]), "boards": anyStringValue(public["board_id"]), "cards": id, "work_metadata": id, "work_observations": anyStringValue(obs["observation"].(map[string]any)["id"]), "documents": anyStringValue(doc["id"]), "topics": anyStringValue(topic.Topic["id"]), "events": anyStringValue(event["id"]), "card_plans": id, "agent_wakeups": "field-wakeup", "work_participants": "field-participant", "runs": "field-run"}
	for table, parent := range map[string]string{"card_revisions": "card_id", "document_revisions": "document_id"} {
		parentID := id
		if table == "document_revisions" {
			parentID = anyStringValue(doc["id"])
		}
		var revision, artifact string
		if err = ws.DB().QueryRow(`SELECT revision_id,artifact_id FROM `+table+` WHERE `+parent+`=? LIMIT 1`, parentID).Scan(&revision, &artifact); err != nil {
			t.Fatal(err)
		}
		fixtures[table] = revision
		if table == "card_revisions" {
			fixtures["artifacts"] = artifact
		}
	}
	for _, source := range resourceaccess.OwnershipSources {
		rowID, ok := fixtures[source.Table]
		if !ok || rowID == "" {
			t.Fatalf("add ownership fixture for %s", source.Table)
		}
		for _, column := range source.Columns {
			t.Run(source.Table+"."+column, func(t *testing.T) {
				query := `SELECT COUNT(*) FROM ` + source.Table + ` WHERE ` + source.ID + `=?`
				var original any
				if err := ws.DB().QueryRow(`SELECT `+column+` FROM `+source.Table+` WHERE `+source.ID+`=?`, rowID).Scan(&original); err != nil {
					t.Fatal(err)
				}
				payload, _ := json.Marshal(map[string]any{"evidence": []any{map[string]any{"ref": " \tCARD :\u00a0" + anyStringValue(private["id"]) + " ", "title": "SECRET"}}})
				if _, err := ws.DB().Exec(`UPDATE `+source.Table+` SET `+column+`=? WHERE `+source.ID+`=?`, string(payload), rowID); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := ws.DB().Exec(`UPDATE `+source.Table+` SET `+column+`=? WHERE `+source.ID+`=?`, original, rowID); err != nil {
						t.Fatal(err)
					}
				}()
				for _, actor := range []string{"stranger", "unauthorized-agent", "owner", "selected-pm"} {
					var count int
					if err := resourceaccess.NewDB(ws.DB()).QueryRowContext(WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected-pm"}), query, rowID).Scan(&count); err != nil {
						t.Fatal(err)
					}
					want := 0
					if actor == "owner" || actor == "selected-pm" {
						want = 1
					}
					if count != want {
						t.Errorf("%s sees %d rows, want %d", actor, count, want)
					}
				}
			})
		}
	}
}

func TestResourceAccessScalarDocumentID(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, id := range []string{"123", "true", "null", "{}", "[]", `"123"`} {
		doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": id, "title": "scalar secret"}, "secret content", "text", []string{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PatchThread(ctx, "owner", anyStringValue(doc["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
		card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "public pin"})
		if err != nil {
			t.Fatal(err)
		}
		if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), "card", anyStringValue(card["id"])) {
			t.Fatalf("unrelated public work hidden by private document ID %q", id)
		}
		if _, err = ws.DB().Exec(`UPDATE cards SET pinned_document_id=? WHERE id=?`, id, card["id"]); err != nil {
			t.Fatal(err)
		}
		for _, actor := range []string{"owner", "stranger", "unauthorized-agent"} {
			scope := WithAccessScope(ctx, AccessScope{ActorID: actor})
			if got := s.CanAccessResource(scope, "card", anyStringValue(card["id"])); got != (actor == "owner") {
				t.Errorf("%s pinned doc %s access=%v", actor, id, got)
			}
		}
	}
}
