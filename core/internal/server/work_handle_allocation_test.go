package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestWorkCreateSucceedsWhenCardRevisionHandlesAreExhausted(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	if _, err := h.workspace.DB().ExecContext(ctx, `
		WITH RECURSIVE seq(n) AS (
			SELECT 1
			UNION ALL
			SELECT n+1 FROM seq WHERE n < 1000
		)
		INSERT INTO artifacts(id, handle, kind, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		SELECT 'exhaust-card-' || n,
		       CASE WHEN n = 1 THEN 'card-revision' ELSE 'card-revision-' || n END,
		       'note', '2026-01-01T00:00:00Z', 'actor-1', 'text', 'seed', '[]', '{}', '[]'
		FROM seq`); err != nil {
		t.Fatal(err)
	}

	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	created := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Ship the fix"}`, http.StatusCreated)
	work := created["work"].(map[string]any)
	cardHandle := strings.TrimPrefix(asString(work["ref"]), "card:")
	var artifactHandle string
	if err := h.workspace.DB().QueryRowContext(ctx, `
		SELECT a.handle
		  FROM cards c
		  JOIN card_revisions cr ON cr.card_id = c.id
		  JOIN artifacts a ON a.id = cr.artifact_id
		 WHERE c.handle = ?`, cardHandle).Scan(&artifactHandle); err != nil {
		t.Fatal(err)
	}
	if artifactHandle != cardHandle+"-r1" {
		t.Fatalf("card artifact handle = %q, want %s-r1", artifactHandle, cardHandle)
	}
	var seeded string
	if err := h.workspace.DB().QueryRowContext(ctx, `SELECT id FROM artifacts WHERE handle = 'card-revision'`).Scan(&seeded); err != nil {
		t.Fatal(err)
	}
	if seeded != "exhaust-card-1" {
		t.Fatalf("existing card-revision handle now points at %q", seeded)
	}
}
