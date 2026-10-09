package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestProjectionEmptyDenialSkipsAtomsAndRechecksEpoch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("empty-denial-%d", i)
		if _, err := ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,'now','fixture','{}')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.DB().Exec(`INSERT INTO derived_topic_views(thread_id,generated_at,data_json) VALUES(?,'now','{}')`, id); err != nil {
			t.Fatal(err)
		}
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	request := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	read := func() int {
		t.Helper()
		var count int
		if err := resourceaccess.NewDB(db).QueryRowContext(request, `SELECT count(*) FROM derived_topic_views`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if read() != 50 {
		t.Fatal("empty-denial fixture lost public projections")
	}
	measure := func() uint64 {
		t.Helper()
		capture.Start()
		if read() != 50 {
			t.Fatal("public projections disappeared")
		}
		capture.Stop()
		if err := capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		return capture.Work().VMSteps
	}
	small := measure()
	values := make([]string, 1000)
	for i := range values {
		values[i] = fmt.Sprintf("atom-%d", i)
	}
	values[0] = "doc:private"
	raw, _ := json.Marshal(map[string]any{"snapshot": values})
	if _, err := ws.DB().Exec(`UPDATE derived_topic_views SET data_json=?`, string(raw)); err != nil {
		t.Fatal(err)
	}
	large := measure()
	t.Logf("empty-denial VM steps: small=%d large=%d", small, large)
	if large > small+1000 {
		t.Fatalf("empty denial scanned projection atoms: small=%d large=%d", small, large)
	}
	// An empty captured snapshot must not bypass a later denial in the same
	// request: the CASE guard consumes the statement's epoch-validated relation.
	if _, err := ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document','private','private','owner')`); err != nil {
		t.Fatal(err)
	}
	if read() != 0 {
		t.Fatal("empty cached snapshot bypassed a later projection denial")
	}
}
