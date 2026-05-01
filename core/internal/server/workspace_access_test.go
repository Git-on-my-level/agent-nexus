package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeWorkspaceAccessMode(t *testing.T) {
	t.Parallel()
	got, err := NormalizeWorkspaceAccessMode("")
	if err != nil || got != WorkspaceAccessModeReadWrite {
		t.Fatalf("empty: got %q err %v", got, err)
	}
	got, err = NormalizeWorkspaceAccessMode("READ_ONLY")
	if err != nil || got != WorkspaceAccessModeReadOnly {
		t.Fatalf("read_only: got %q err %v", got, err)
	}
	_, err = NormalizeWorkspaceAccessMode("invalid")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestEnrichRouteMutationRecoveryPostArchive(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/topics/t1/archive", nil)
	base := routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
	got := enrichRouteMutationPolicy(req, base)
	if got.mutation != routeMutationRecovery {
		t.Fatalf("expected recovery, got %q", got.mutation)
	}
}

func TestWorkspaceReadOnlyGateBlocksBusinessMutation(t *testing.T) {
	t.Parallel()
	opts := handlerOptions{
		workspaceAccessMode: WorkspaceAccessModeReadOnly,
	}
	rr := httptest.NewRecorder()
	ok := enforceWorkspaceWriteAccess(rr, opts, routeAccessRequirement{
		bucket:    routeAccessWorkspaceBusiness,
		mutation:  routeMutationBusiness,
		supported: true,
	})
	if ok {
		t.Fatal("expected gate to reject business mutation")
	}
	if rr.Code != 423 {
		t.Fatalf("expected 423 Locked, got %d", rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "workspace_read_only" {
		t.Fatalf("expected workspace_read_only error, payload=%#v", payload)
	}
	if payload["access_mode"] != WorkspaceAccessModeReadOnly {
		t.Fatalf("expected access_mode detail at payload root, payload=%#v", payload)
	}
}
