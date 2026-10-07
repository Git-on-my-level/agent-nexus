package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func TestInboxStreamTickHiddenVolumeBudget(t *testing.T) {
	requireIntegrationTest(t)
	for _, fixture := range []struct {
		name                     string
		visible, private, hidden int
	}{
		{"visible_104", 104, 0, 0},
		{"private_896_visible_104", 104, 896, 0},
		{"hidden_896_visible_104", 104, 0, 896},
		{"hidden_9896_visible_104", 104, 0, 9896},
		{"private_896_empty", 0, 896, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			store := env.primitiveStore.(*primitives.Store)
			public := seedStreamPrivacyThread(t, store, "owner", false)
			private := seedStreamPrivacyThread(t, store, "owner", true)
			hidden := seedStreamPrivacyThread(t, store, "owner", false)
			if _, err := store.ArchiveThread(context.Background(), "owner", hidden); err != nil {
				t.Fatal(err)
			}
			items := []primitives.DerivedInboxItem{}
			for i := 0; i < fixture.visible; i++ {
				items = append(items, streamPrivacyInboxItem(public, fmt.Sprintf("visible-%05d", i), "Public ask"))
			}
			for i := 0; i < fixture.hidden; i++ {
				item := streamPrivacyInboxItem(public, fmt.Sprintf("hidden-%05d", i), "Hidden notification")
				item.Category = "escalate"
				item.Data["kind"] = "agent_wake"
				item.Data["subject_ref"] = "thread:" + hidden
				item.Data["related_refs"] = []any{"thread:" + hidden}
				items = append(items, item)
			}
			seedStreamPrivacyInbox(t, store, public, items...)
			items = nil
			for i := 0; i < fixture.private; i++ {
				items = append(items, streamPrivacyInboxItem(private, fmt.Sprintf("private-%05d", i), "Private secret"))
			}
			seedStreamPrivacyInbox(t, store, private, items...)
			db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
			defer db.Close()
			measured := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
			req := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: "reader"}))
			var samples []time.Duration
			for tick := 0; tick < 3; tick++ {
				counter.Reset()
				start := time.Now()
				rows, page, err := loadInboxStreamPage(req, handlerOptions{primitiveStore: measured}, primitives.DerivedInboxListFilter{})
				records := buildInboxStreamRecords(rows)
				elapsed := time.Since(start)
				if err != nil || len(records) != fixture.visible || page.More {
					t.Fatalf("tick %d: visible=%d partial=%t err=%v", tick, len(records), page.More, err)
				}
				if counter.Count() > 3 {
					t.Fatalf("unbounded tick statements: %d", counter.Count())
				}
				t.Logf("tick=%d elapsed=%s statements=%d rows=%d visible=%d", tick, elapsed, counter.Count(), counter.ReturnedRows(), len(records))
				for j, statement := range counter.Statements() {
					t.Logf("statement=%d elapsed=%s sql_bytes=%d", j, statement.Elapsed, len(statement.SQL))
				}
				samples = append(samples, elapsed)
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			t.Logf("median=%s", samples[1])
			if fixture.hidden == 9896 && samples[1] > 50*time.Millisecond {
				t.Fatalf("hidden-volume tick median %s exceeds 50 ms", samples[1])
			}
		})
	}
}
