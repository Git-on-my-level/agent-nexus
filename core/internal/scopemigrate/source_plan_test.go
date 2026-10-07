package scopemigrate

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/storage"
)

func TestMetadataSourcePlanUsesBoundedRIDCandidates(t *testing.T) {
	ctx := context.Background()
	w, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err = scopedrepo.New(w.DB()).Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := w.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = storage.InstallScopeMigrationEpoch(ctx, tx); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN QUERY PLAN "+metadataPageSQL, 128, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	s := strings.Join(plan, "\n")
	for _, want := range []string{"MATERIALIZE candidates", "SEARCH scope_resource_rids USING INTEGER PRIMARY KEY (rowid>?)", "SEARCH r USING PRIMARY KEY", "SEARCH c USING INTEGER PRIMARY KEY"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing indexed bound %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "SCAN r") || strings.Contains(s, "SCAN c") {
		t.Fatal("unbounded joined source scan", s)
	}
	t.Log(s)
}
