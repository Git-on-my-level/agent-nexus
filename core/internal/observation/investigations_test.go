package observation

import (
	"context"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestSavedInvestigationsRetainScopePresetAndVersionAcrossRestart(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resolve := func(ctx context.Context, preset string) error {
		if preset != "approved-existing-agent" {
			return errors.New("missing preset")
		}
		return nil
	}
	store, err := NewInvestigationStore(db, resolve)
	if err != nil {
		t.Fatal(err)
	}
	spec := InvestigationSpec{Objective: "Inspect deployment evidence", Target: fixtureTarget(), PresetRef: "approved-existing-agent", AllowedSources: []string{"fixture"}, EvidenceRequirements: []string{"serving revision", "traffic revision"}, Limits: Limits{Timeout: time.Minute, MaxOutputBytes: 65536, MaxRequests: 10, MaxTokens: 1000}, Interval: time.Hour}
	saved, err := store.Save(context.Background(), "w", "investigation-1", spec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 {
		t.Fatal("missing initial version")
	}
	reopened, err := NewInvestigationStore(db, resolve)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(context.Background(), "w", "investigation-1")
	if err != nil || got.Spec.PresetRef != spec.PresetRef {
		t.Fatalf("restart lost specification: %+v %v", got, err)
	}
	if _, err := reopened.Get(context.Background(), "other", "investigation-1"); err == nil {
		t.Fatal("cross workspace spec exposed")
	}
	if _, err := reopened.Save(context.Background(), "w", "investigation-1", spec, 0); err == nil {
		t.Fatal("stale specification update accepted")
	}
	spec.PresetRef = "invented-provider"
	if _, err := reopened.Save(context.Background(), "w", "investigation-1", spec, 1); err == nil {
		t.Fatal("unknown execution preset accepted")
	}
}
