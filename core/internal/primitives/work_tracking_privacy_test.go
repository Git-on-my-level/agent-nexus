package primitives

import (
	"context"
	"testing"
	"time"
)

func TestWorkTrackingColumnsInheritCardAndFallbackGrantsNoAccess(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private tracking"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "owner", workString(board["id"]), map[string]any{"title": "Private responsibility"})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(card["id"])
	s.SetTrackingLead(func() string { return "stranger" })
	if _, err = s.AcquireCardLease(ctx, "owner", id, CardLeaseAcquireInput{Owner: "lane", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReleaseCardLease(ctx, "owner", id, CardLeaseReleaseInput{Owner: "lane"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWork(ctx, id)
	if err != nil || got["owner"] != "actor:owner" {
		t.Fatal("unreadable lead received responsibility", got, err)
	}
	rows, err := ws.DB().Query(`PRAGMA table_info(work_tracking)`)
	if err != nil {
		t.Fatal(err)
	}
	columns := []string{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var def any
		if err = rows.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, column := range columns {
		for _, actor := range []string{"owner", "stranger", "unauthorized-agent"} {
			read := WithAccessScope(ctx, AccessScope{ActorID: actor})
			result, err := s.db.QueryContext(read, `SELECT `+column+` FROM work_tracking WHERE card_id=?`, id)
			if err != nil {
				t.Fatal(err)
			}
			visible := result.Next()
			result.Close()
			if visible != (actor == "owner") {
				t.Fatalf("column %s visible to %s=%v", column, actor, visible)
			}
		}
	}
	read := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	digest, err := s.TrackingDigestPreview(read, "stranger")
	if err != nil || len(digest) != 0 {
		t.Fatalf("private digest leaked: %v %v", digest, err)
	}
}
