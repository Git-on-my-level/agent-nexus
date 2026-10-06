package perfguard

import (
	"agent-nexus-core/internal/storage"
	"context"
	"os"
	"testing"
	"time"
)

func TestPerformanceScaleFixture(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("scale integration fixture")
	}
	ctx := context.Background()
	w, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := Seed(ctx, w.DB(), w.Layout().ArtifactContentDir, "scale-owner", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"cards", "documents", "artifacts", "events", "card_plans", "series_points", "derived_inbox_items", "agents", "runs", "host_agents"} {
		var n int
		if err := w.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != Rows {
			t.Fatalf("%s=%d, want %d", table, n, Rows)
		}
	}
	var pmCount int
	if err := w.DB().QueryRow("SELECT COUNT(*) FROM pm_records").Scan(&pmCount); err != nil {
		t.Fatal(err)
	}
	if pmCount != Rows*4 {
		t.Fatalf("pm_records=%d, want %d", pmCount, Rows*4)
	}

}
