package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResourceAccessScopeInheritanceAndMutationGuards(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "private board"})
	if err != nil {
		t.Fatal(err)
	}
	b := anyStringValue(board["id"])
	card, err := s.CreateBoardCard(ctx, "owner", b, AddBoardCardInput{Title: "private card", Body: "secret", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	id, thread := anyStringValue(card.Card["id"]), anyStringValue(card.Card["thread_id"])
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	ref := anyStringValue(card.Card["ref"])
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{ref}, "payload": map[string]any{"text": "secret without thread"}})
	if err != nil {
		t.Fatal(err)
	}
	alias := "old-private-card"
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card',?,?,?,?)`, alias, id, anyStringValue(card.Card["handle"]), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	artifact, err := s.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "refs": []string{"card:" + alias}}, "secret artifact", "text")
	if err != nil {
		t.Fatal(err)
	}
	// Revision IDs and computed handles propagate through threadless evidence.
	var revisionID string
	if err = ws.DB().QueryRowContext(ctx, `SELECT revision_id FROM card_revisions WHERE card_id=? LIMIT 1`, id).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"card_revision:" + revisionID, "card_revision:" + anyStringValue(card.Card["handle"]) + "-r1"} {
		revisionEvent, e := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{ref}, "payload": map[string]any{"text": "private revision evidence"}})
		if e != nil {
			t.Fatal(e)
		}
		if s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), "event", anyStringValue(revisionEvent["id"])) {
			t.Errorf("revision leak: %s", ref)
		}
	}
	// Fold reference case consistently; NULL legacy handles must not poison
	// unrelated rows through SQL NOT IN semantics.
	if _, err = ws.DB().ExecContext(ctx, `UPDATE events SET handle=NULL WHERE id=?`, event["id"]); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ id, ref string }{{"hidden-run", strings.ToUpper(ref)}, {"public-run", ""}} {
		if _, err = ws.DB().ExecContext(ctx, `INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,state,liveness,result_collected,labels_json,card_ref,last_observed_at) VALUES(?,?,'test',?,'host','agent','test','running','alive',0,'[]',?,'now')`, r.id, r.id, r.id, r.ref); err != nil {
			t.Fatal(err)
		}
	}
	var runIDs string
	if err = s.db.QueryRowContext(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), `SELECT group_concat(id) FROM runs`).Scan(&runIDs); err != nil {
		t.Fatal(err)
	}
	if runIDs != "public-run" {
		t.Fatalf("scoped runs: %s", runIDs)
	}
	for _, actor := range []string{"stranger", "unselected-agent", ""} {
		scoped := WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected-pm"})
		for _, r := range []ResourceRefInput{{"card", id}, {"card", ref}, {"card", "card:" + alias}, {"thread", thread}, {"board", b}, {"event", anyStringValue(event["id"])}, {"artifact", anyStringValue(artifact["id"])}} {
			if s.CanAccessResource(scoped, r.Type, r.Ref) {
				t.Errorf("%s can access %#v", actor, r)
			}
		}
		// Loader authorization and lower-level writes must independently reject.
		due := "2030-01-01T00:00:00Z"
		if _, err = s.UpdateBoardCard(scoped, "mutation-actor", b, id, UpdateBoardCardInput{DueAt: &due}); !errors.Is(err, ErrNotFound) {
			t.Errorf("update returned %v", err)
		}
		if _, err = s.db.ExecContext(scoped, `UPDATE cards SET summary=? WHERE id=?`, "changed", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("raw update returned %v", err)
		}
		refs, _ := json.Marshal([]string{ref})
		if _, err = s.db.ExecContext(scoped, `INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,?,?,?,?,?)`, "injected-"+actor, "message_posted", time.Now().UTC().Format(time.RFC3339Nano), actor, string(refs), `{"text":"injected"}`); !errors.Is(err, ErrNotFound) {
			t.Errorf("JSON mutation returned %v", err)
		}
	}
	for _, actor := range []string{"owner", "selected-pm"} {
		if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected-pm"}), "card", id) {
			t.Errorf("authorized %s lost card", actor)
		}
	}
	current, err := s.GetBoardCard(ctx, b, id)
	if err != nil {
		t.Fatal(err)
	}
	if anyStringValue(current["summary"]) != "secret" || anyStringValue(current["due_at"]) != "" {
		t.Fatalf("denied mutation persisted: %#v", current)
	}
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	public, err := s.CreateBoard(scoped, "stranger", map[string]any{"title": "private board"})
	if err != nil {
		t.Fatalf("private namespace collision broke creation: %v", err)
	}
	if public["handle"] == board["handle"] {
		t.Fatal("reused private handle")
	}
}

func TestResourceAccessScopeFiltersBeforeAggregationAndCursor(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := s.CreateThread(ctx, "owner", map[string]any{"title": "public"})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err = s.CreateThread(ctx, "owner", map[string]any{"title": "private", "pm_actor_id": "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	limit := 1
	rows, cursor, err := s.ListThreads(scoped, ThreadListFilter{Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != public.Thread["id"] || cursor != "" {
		t.Fatalf("hidden rows affected page: %#v cursor=%q", rows, cursor)
	}
	var count int
	if err = s.db.QueryRowContext(scoped, "SELECT\nCOUNT(*) FROM threads").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("private count: %d", count)
	}
	// A mixed projection's canonical totals stay intact, but are not reader output.
	hidden, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "thread_id": public.Thread["id"], "refs": []string{}, "payload": map[string]any{"text": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateThread(ctx, "owner", map[string]any{"title": "private subject", "pm_actor_id": "owner"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.DB().ExecContext(ctx, `INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at) VALUES('mixed-edge','event',?,'thread',?,'ref',?)`, hidden["id"], private.Thread["id"], time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	projection := DerivedTopicProjection{ThreadID: anyStringValue(public.Thread["id"]), InboxCount: 9, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{}}
	if err = s.PutDerivedTopicProjection(ctx, projection); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetDerivedTopicProjection(scoped, projection.ThreadID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mixed projection exposed: %v", err)
	}
	saved, err := s.GetDerivedTopicProjection(ctx, projection.ThreadID)
	if err != nil || saved.InboxCount != 9 {
		t.Fatalf("canonical projection changed: %#v %v", saved, err)
	}
}

func TestResourceAccessPurgeRetainsEvidenceAndSharedBlobs(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateThread(ctx, "owner", map[string]any{"title": "purged privacy", "pm_actor_id": "owner"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(private.Thread["id"])
	evidence, err := s.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "refs": []string{"thread:" + id}}, "shared blob", "text")
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateArtifact(ctx, "stranger", map[string]any{"kind": "note", "refs": []string{}}, "shared blob", "text")
	if err != nil {
		t.Fatal(err)
	}
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if _, err = s.TrashArtifact(scoped, "stranger", anyStringValue(public["id"]), "cleanup"); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeTrashedArtifact(scoped, anyStringValue(public["id"])); err != nil {
		t.Fatal(err)
	}
	content, _, err := s.GetArtifactContent(ctx, anyStringValue(evidence["id"]))
	if err != nil || string(content) != "shared blob" {
		t.Fatalf("shared private blob lost: %q %v", content, err)
	}
	visibleUsage, err := s.GetWorkspaceUsageSummary(scoped)
	if err != nil {
		t.Fatal(err)
	}
	if visibleUsage.Usage.BlobObjects != 0 || visibleUsage.Usage.BlobBytes != 0 || visibleUsage.Usage.Artifacts != 0 {
		t.Fatalf("private usage exposed: %#v", visibleUsage.Usage)
	}
	readRebuild, err := s.RebuildBlobUsageLedger(scoped)
	if err != nil {
		t.Fatal(err)
	}
	if readRebuild.BlobObjects != 0 || readRebuild.CanonicalHashes != 0 || readRebuild.BlobBytes != 0 {
		t.Fatalf("rebuild exposed private totals: %#v", readRebuild)
	}
	s.quota.MaxArtifacts = 1
	quotaErr := s.checkWorkspaceWriteQuota(scoped, 0, quotaWriteDelta{artifacts: 1}, blobLedgerWritePlan{})
	var violation *QuotaViolation
	if !errors.As(quotaErr, &violation) || violation.Current != 0 || violation.Projected != 1 {
		t.Fatalf("quota did not enforce global limit with scoped totals: %#v", quotaErr)
	}
	s.quota.MaxArtifacts = 0

	var privateHash string
	if err = ws.DB().QueryRowContext(ctx, `SELECT content_hash FROM artifacts WHERE id=?`, evidence["id"]).Scan(&privateHash); err != nil {
		t.Fatal(err)
	}
	s.quota.MaxBlobBytes = 1
	quotaErr = s.checkWorkspaceWriteQuota(scoped, 11, quotaWriteDelta{dbBytes: 2}, blobLedgerWritePlan{contentHash: privateHash, ledgerPresent: true})
	if !errors.As(quotaErr, &violation) || violation.Projected-violation.Current != 13 {
		t.Fatalf("private dedup exposed: %#v", quotaErr)
	}
	s.quota.MaxBlobBytes = 0

	canonicalUsage, err := s.GetWorkspaceUsageSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if canonicalUsage.Usage.BlobObjects != 1 {
		t.Fatalf("canonical ledger changed: %#v", canonicalUsage.Usage)
	}

	if _, err = s.TrashThread(ctx, "owner", id, "cleanup"); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeThread(ctx, id); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(scoped, "artifact", anyStringValue(evidence["id"])) {
		t.Fatal("purge declassified orphaned evidence")
	}
	fresh, err := s.CreateThread(scoped, "stranger", map[string]any{"title": "purged privacy"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Thread["handle"] == private.Thread["handle"] {
		t.Fatal("purged private handle reused")
	}
	if err = s.CheckResourceValues(scoped, []string{anyStringValue(fresh.Thread["ref"])}); err != nil {
		t.Fatalf("public replacement inaccessible: %v", err)
	}
}
