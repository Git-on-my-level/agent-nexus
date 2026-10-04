package server

import (
	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

type reportScopeStore struct {
	*primitives.Store
	filters []primitives.ReportWorkFilter
}

func (s *reportScopeStore) ListReportWork(ctx context.Context, filter primitives.ReportWorkFilter) (primitives.ReportWorkPage, error) {
	s.filters = append(s.filters, filter)
	return s.Store.ListReportWork(ctx, filter)
}
func TestReportPanelsShareBoundedReadsByScope(t *testing.T) {
	h := newPrimitivesTestServer(t)
	board, err := h.primitiveStore.CreateBoard(context.Background(), "actor", map[string]any{"title": "Selected"})
	if err != nil {
		t.Fatal(err)
	}
	store := &reportScopeStore{Store: h.primitiveStore.(*primitives.Store)}
	reader := reportReader{r: httptest.NewRequest("GET", "/", nil), opts: handlerOptions{primitiveStore: store}, now: time.Now()}
	for _, kind := range []string{"live-initiatives", "live-work-mix"} {
		if _, _, err := reader.materialize(reports.Panel{Type: kind, Query: reports.Query{Limit: 10, Sort: "priority", GroupBy: "phase"}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.filters) != 1 || store.filters[0].Limit != reports.MaxRows {
		t.Fatalf("shared default scope: %#v", store.filters)
	}
	for _, kind := range []string{"live-initiatives", "live-work-mix"} {
		if _, _, err := reader.materialize(reports.Panel{Type: kind, Query: reports.Query{BoardRefs: []string{anyString(board["ref"])}, Limit: 10, Sort: "priority", GroupBy: "phase"}}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.filters) != 2 || len(store.filters[1].BoardIDs) != 1 || store.filters[1].BoardIDs[0] != anyString(board["id"]) {
		t.Fatalf("scope not pushed into store: %#v", store.filters)
	}
}
