package primitives

import (
	"context"
	"testing"
)

func TestDashboardPinsPrincipalIsolationAndFallback(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Dashboard","summary":"Current work","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"work","title":"Work","summary":"Work","outcome":"Ship"}],"sources":[],"panels":[{"id":"work","project_id":"work","type":"live-initiatives","title":"Work","author":"Owner","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	first, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Default"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Second"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetWorkspaceDashboard(ctx, "owner", workString(first["id"])); err != nil {
		t.Fatal(err)
	}
	owner := WithAccessScope(ctx, AccessScope{ActorID: "owner"})
	other := WithAccessScope(ctx, AccessScope{ActorID: "other-agent"})
	data, err := s.DashboardPins(other)
	if err != nil || data["source"] != "workspace_default" {
		t.Fatal(data, err)
	}
	refs := []string{workString(second["ref"]), workString(first["ref"])}
	if err = s.SetDashboardPins(owner, refs); err != nil {
		t.Fatal(err)
	}
	data, err = s.DashboardPins(owner)
	if err != nil || data["source"] != "personal" || data["document_refs"].([]string)[0] != refs[0] {
		t.Fatal(data, err)
	}
	if err = s.SetDashboardPins(owner, []string{refs[0], refs[0]}); err == nil {
		t.Fatal("duplicate accepted")
	}
	data, err = s.DashboardPins(other)
	if err != nil || data["source"] != "workspace_default" || len(data["document_refs"].([]string)) != 1 {
		t.Fatal(data, err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(second["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SetDashboardPins(other, []string{refs[0]}); err == nil {
		t.Fatal("reader pinned inaccessible document")
	}
	// A visibility change filters existing pins and cannot expose their titles.
	if _, err = s.PatchThread(ctx, "owner", workString(first["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	data, err = s.DashboardPins(other)
	if err != nil || len(data["document_refs"].([]string)) != 0 {
		t.Fatal(data, err)
	}
	if err = s.SetDashboardPins(owner, []string{}); err != nil {
		t.Fatal(err)
	}
	data, err = s.DashboardPins(owner)
	if err != nil || data["source"] != "workspace_default" {
		t.Fatal(data, err)
	}
}
