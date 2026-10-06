package primitives

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

func TestEvidenceProjectionIDsDoNotBecomeExternalResourceIdentities(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateWork(ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": "Private", "source_refs": []any{map[string]any{"authority": "generic", "connection_id": "private", "native_id": "private-evidence"}}})
	if err != nil {
		t.Fatal(err)
	}
	var recordID int64
	if err = ws.DB().QueryRow(`SELECT id FROM work_evidence_records WHERE card_id=? LIMIT 1`, private["id"]).Scan(&recordID); err != nil {
		t.Fatal(err)
	}
	nativeID := strconv.FormatInt(recordID, 10)
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public evidence"})
	if err != nil {
		t.Fatal(err)
	}
	scope := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	public, err := s.CreateWork(scope, "stranger", anyStringValue(publicBoard["id"]), map[string]any{"title": "Public numeric identity", "source": map[string]any{"authority": "generic", "connection_id": "public", "native_id": nativeID}})
	if err != nil {
		t.Fatalf("internal record ID constrained a public mutation: %v", err)
	}
	if _, err = s.GetWork(scope, anyStringValue(public["id"])); err != nil {
		t.Fatal(err)
	}
	previews, err := s.ResolveRefs(scope, []string{"generic:" + nativeID}, nil, time.Now(), 0)
	if err != nil || !previews[0].Resolvable || previews[0].ConnectionID != "public" {
		t.Fatalf("numeric public identity hidden by private projection: %+v %v", previews, err)
	}
}

func TestSourceRefsAndEvidenceProjectionsUseCentralOwnership(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateWork(ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": "Private source", "source": map[string]any{"authority": "generic", "connection_id": "private", "native_id": "private", "url": "https://source.test/private"}})
	if err != nil {
		t.Fatal(err)
	}
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateWork(ctx, "owner", anyStringValue(publicBoard["id"]), map[string]any{"title": "Public source"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(public["id"])
	refs := []any{map[string]any{"authority": "generic", "connection_id": "private", "native_id": "evidence", "url": "https://source.test/private", "identifier_aliases": []string{"private-alias"}, "title": "Private evidence", "extension": map[string]any{"ref": private["ref"]}}}
	for _, actor := range []string{"stranger", "unauthorized-agent"} {
		scope := WithAccessScope(ctx, AccessScope{ActorID: actor})
		if _, err = s.PatchWork(scope, actor, id, 1, map[string]any{"source_refs": refs}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s attached private source refs: %v", actor, err)
		}
	}
	if _, err = s.PatchWork(WithAccessScope(ctx, AccessScope{ActorID: "owner"}), "owner", id, 1, map[string]any{"source_refs": refs}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"stranger", "unauthorized-agent", "owner", "selected-pm"} {
		scope := WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected-pm"})
		allowed := actor == "owner" || actor == "selected-pm"
		_, err := s.GetWork(scope, id)
		if allowed && err != nil || !allowed && !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s work access: %v", actor, err)
		}
		previews, err := s.ResolveRefs(scope, []string{"private-alias"}, nil, time.Now(), 0)
		if err != nil || previews[0].Resolvable != allowed {
			t.Fatalf("%s evidence resolution: %+v %v", actor, previews, err)
		}
		for _, table := range []string{"work_evidence_records", "work_evidence_index", "work_evidence_entries", "work_evidence_keys"} {
			var count int
			if err := s.db.QueryRowContext(scope, `SELECT count(*) FROM `+table+` WHERE card_id=?`, id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count > 0) != allowed {
				t.Fatalf("%s %s count=%d", actor, table, count)
			}
		}
	}
}
