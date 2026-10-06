package primitives

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

func TestEvidenceStructuralWritesInvalidateRequestSnapshot(t *testing.T) {
	for _, table := range []string{"work_evidence_records", "work_evidence_index"} {
		t.Run(table, func(t *testing.T) {
			ctx := context.Background()
			ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public", "source_refs": []any{map[string]any{"authority": "generic", "connection_id": "public", "native_id": "snapshot-key"}}})
			if err != nil {
				t.Fatal(err)
			}
			request := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
			assertResolved := func(want bool) {
				t.Helper()
				got, err := s.ResolveRefs(request, []string{"snapshot-key"}, nil, time.Now(), 0)
				if err != nil || len(got) != 1 || got[0].Resolvable != want {
					t.Fatalf("resolved=%+v want=%v err=%v", got, want, err)
				}
			}
			assertResolved(true)
			if denialSnapshotFrom(request) == nil {
				t.Fatal("missing request snapshot")
			}
			// Only a structural parent changes: ref-bearing payload/key atoms stay
			// identical, so edge write triggers alone cannot invalidate this cache.
			if _, err = ws.DB().Exec(`UPDATE `+table+` SET card_id=? WHERE card_id=?`, private["id"], public["id"]); err != nil {
				t.Fatal(err)
			}
			assertResolved(false)
			if _, err = ws.DB().Exec(`UPDATE `+table+` SET card_id=? WHERE card_id=?`, public["id"], private["id"]); err != nil {
				t.Fatal(err)
			}
			assertResolved(true)
		})
	}
}

func TestSharedSourceURLPublicationSurvivesPrivatePublisherAndPurge(t *testing.T) {
	for _, size := range []int{32, 2100} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			ctx := context.Background()
			ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			url := "https://source.test/" + strings.Repeat("x", size)
			private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public"})
			if err != nil {
				t.Fatal(err)
			}
			// Raw import models released legacy URLs longer than new alias caps.
			for _, card := range []map[string]any{private, public} {
				if _, err = ws.DB().Exec(`UPDATE work_metadata SET authority='generic',native_id=card_id,metadata_json=json_set(metadata_json,'$.source.url',?) WHERE card_id=?`, url, card["id"]); err != nil {
					t.Fatal(err)
				}
			}
			event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"subject_ref": url}})
			if err != nil {
				t.Fatal(err)
			}
			assertAccess := func() {
				t.Helper()
				scope := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
				if !s.CanAccessResource(scope, "card", public["id"].(string)) {
					t.Fatal("private URL publisher suppressed public publisher")
				}
				if s.CanAccessResource(scope, "event", event["id"].(string)) {
					t.Fatal("private URL reference exposed")
				}
			}
			assertAccess()
			if _, err = s.ArchiveBoardCard(ctx, "owner", "", private["id"].(string), RemoveBoardCardInput{}); err != nil {
				t.Fatal(err)
			}
			if err = s.PurgeArchivedBoardCard(ctx, "", private["id"].(string)); err != nil {
				t.Fatal(err)
			}
			assertAccess()
		})
	}
}

func TestRevisionLifecycleUsesIndexedIdentityCandidates(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, kind := range []string{"card", "document"} {
		for _, ref := range []string{kind + "_revision:direct-id", kind + "_revision:Legacy._/Name-r7"} {
			rows, err := ws.DB().Query(`EXPLAIN QUERY PLAN SELECT `+nativeReferenceLifecycleSQL(ctx, "?1", true), ref)
			if err != nil {
				t.Fatal(err)
			}
			indexed := false
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(detail, "SCAN access_revision") {
					t.Fatalf("workspace revision scan: %s", detail)
				}
				if strings.Contains(detail, "idx_access_identity_key") {
					indexed = true
				}
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if !indexed {
				t.Fatal("revision lookup omitted fixed-key identity index")
			}
		}
	}
}
