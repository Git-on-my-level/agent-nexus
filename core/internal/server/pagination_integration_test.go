package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"organization-autorunner-core/internal/actors"
)

func TestActorListPaginationLimitParameter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	for i := 0; i < 5; i++ {
		postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-`+string(rune('b'+i))+`","display_name":"Actor `+string(rune('B'+i))+`","created_at":"2026-03-04T`+strconv.Itoa(10+i)+`:00:00Z"}}`, http.StatusCreated)
	}

	resp, err := http.Get(h.baseURL + "/actors?limit=3")
	if err != nil {
		t.Fatalf("GET /actors with limit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", resp.StatusCode)
	}

	var payload struct {
		Actors     []actors.Actor `json:"actors"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode actors response: %v", err)
	}

	if len(payload.Actors) != 3 {
		t.Fatalf("expected 3 actors with limit=3, got %d", len(payload.Actors))
	}
	if payload.NextCursor == "" {
		t.Fatal("expected next_cursor when more results exist")
	}
}

func TestActorListPaginationCursorStability(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)

	for i := 0; i < 5; i++ {
		postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-`+string(rune('a'+i))+`","display_name":"Actor `+string(rune('A'+i))+`","created_at":"2026-03-04T`+strconv.Itoa(10+i)+`:00:00Z"}}`, http.StatusCreated)
	}

	var allActors []actors.Actor
	cursor := ""
	for {
		url := h.baseURL + "/actors?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /actors: %v", err)
		}

		var payload struct {
			Actors     []actors.Actor `json:"actors"`
			NextCursor string         `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode actors response: %v", err)
		}
		resp.Body.Close()

		allActors = append(allActors, payload.Actors...)

		if payload.NextCursor == "" {
			break
		}
		cursor = payload.NextCursor
	}

	if len(allActors) != 5 {
		t.Fatalf("expected 5 total actors, got %d", len(allActors))
	}

	seen := make(map[string]bool)
	for _, a := range allActors {
		if seen[a.ID] {
			t.Fatalf("duplicate actor in paginated results: %s", a.ID)
		}
		seen[a.ID] = true
	}

	expectedOrder := []string{"actor-a", "actor-b", "actor-c", "actor-d", "actor-e"}
	for i, id := range expectedOrder {
		if allActors[i].ID != id {
			t.Fatalf("expected actor %s at position %d, got %s", id, i, allActors[i].ID)
		}
	}
}

func TestActorListSearchQuery(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"alice","display_name":"Alice Smith","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"bob","display_name":"Bob Jones","created_at":"2026-03-04T11:00:00Z"}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"charlie","display_name":"Charlie Brown","created_at":"2026-03-04T12:00:00Z"}}`, http.StatusCreated)

	resp, err := http.Get(h.baseURL + "/actors?q=alice")
	if err != nil {
		t.Fatalf("GET /actors with search: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Actors []actors.Actor `json:"actors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode actors response: %v", err)
	}

	if len(payload.Actors) != 1 {
		t.Fatalf("expected 1 actor matching 'alice', got %d", len(payload.Actors))
	}
	if payload.Actors[0].ID != "alice" {
		t.Fatalf("expected alice, got %s", payload.Actors[0].ID)
	}
}

func TestActorListSearchByID(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"user-123","display_name":"User One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"other-user","display_name":"Other User","created_at":"2026-03-04T11:00:00Z"}}`, http.StatusCreated)

	resp, err := http.Get(h.baseURL + "/actors?q=123")
	if err != nil {
		t.Fatalf("GET /actors with search: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Actors []actors.Actor `json:"actors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode actors response: %v", err)
	}

	if len(payload.Actors) != 1 {
		t.Fatalf("expected 1 actor matching '123', got %d", len(payload.Actors))
	}
	if payload.Actors[0].ID != "user-123" {
		t.Fatalf("expected user-123, got %s", payload.Actors[0].ID)
	}
}

func TestActorListInvalidLimit(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	testCases := []struct {
		limit   string
		wantErr bool
	}{
		{"0", true},
		{"-1", true},
		{"1001", true},
		{"abc", true},
		{"", false},
		{"1", false},
		{"1000", false},
	}

	for _, tc := range testCases {
		url := h.baseURL + "/actors"
		if tc.limit != "" {
			url += "?limit=" + tc.limit
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /actors with limit=%s: %v", tc.limit, err)
		}
		resp.Body.Close()

		if tc.wantErr && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%s: expected status 400, got %d", tc.limit, resp.StatusCode)
		}
		if !tc.wantErr && resp.StatusCode != http.StatusOK {
			t.Errorf("limit=%s: expected status 200, got %d", tc.limit, resp.StatusCode)
		}
	}
}

func TestThreadListPaginationLimitParameter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	for i := 0; i < 5; i++ {
		integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-"+string(rune('a'+i)), "Thread "+string(rune('A'+i))))
	}

	resp, err := http.Get(h.baseURL + "/threads?limit=3")
	if err != nil {
		t.Fatalf("GET /threads with limit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", resp.StatusCode)
	}

	var payload struct {
		Threads    []map[string]any `json:"threads"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode threads response: %v", err)
	}

	if len(payload.Threads) != 3 {
		t.Fatalf("expected 3 threads with limit=3, got %d", len(payload.Threads))
	}
	if payload.NextCursor == "" {
		t.Fatal("expected next_cursor when more results exist")
	}
}

func TestThreadListPaginationWithStaleFilter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	for i := 0; i < 3; i++ {
		integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-"+string(rune('a'+i)), "Thread "+string(rune('A'+i))))
	}

	integrationPatchThread(t, h, "actor-1", "thread-a", map[string]any{"title": "Thread A refreshed"}, nil)
	postJSONExpectStatus(t, h.baseURL+"/derived/rebuild", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	var staleIDs []string
	cursor := ""
	for {
		url := h.baseURL + "/threads?stale=true&limit=1"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /threads?stale=true: %v", err)
		}

		var payload struct {
			Threads    []map[string]any `json:"threads"`
			NextCursor string           `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode stale thread page: %v", err)
		}
		resp.Body.Close()

		if len(payload.Threads) == 0 {
			t.Fatalf("expected stale-filtered page to contain results before cursor exhaustion, cursor=%q", cursor)
		}

		for _, thread := range payload.Threads {
			id, ok := thread["id"].(string)
			if !ok {
				t.Fatalf("thread id is not a string: %#v", thread["id"])
			}
			stale, ok := thread["stale"].(bool)
			if !ok || !stale {
				t.Fatalf("expected stale=true thread payload, got %#v", thread)
			}
			staleIDs = append(staleIDs, id)
		}

		if payload.NextCursor == "" {
			break
		}
		cursor = payload.NextCursor
	}

	if len(staleIDs) != 2 {
		t.Fatalf("expected 2 stale threads across paginated results, got %d (%v)", len(staleIDs), staleIDs)
	}
	for _, id := range staleIDs {
		if id == "thread-a" {
			t.Fatalf("did not expect refreshed thread in stale-filtered results: %v", staleIDs)
		}
	}
}

func TestThreadListSearchQuery(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("search-thread", "Search Test Thread"))
	integrationSeedThread(t, h, "actor-1", paginationTestThread("other-thread", "Other Thread"))

	resp, err := http.Get(h.baseURL + "/threads?q=search")
	if err != nil {
		t.Fatalf("GET /threads with search: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode threads response: %v", err)
	}

	if len(payload.Threads) != 1 {
		t.Fatalf("expected 1 thread matching 'search', got %d", len(payload.Threads))
	}
	if payload.Threads[0]["id"] != "search-thread" {
		t.Fatalf("expected search-thread, got %v", payload.Threads[0]["id"])
	}
}

func TestDocumentListPaginationLimitParameter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))

	for i := 0; i < 5; i++ {
		postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"document_id":"doc-`+string(rune('a'+i))+`","title":"Doc `+string(rune('A'+i))+`"},"content":"content","content_type":"text"}`, http.StatusCreated)
	}

	resp, err := http.Get(h.baseURL + "/docs?limit=3")
	if err != nil {
		t.Fatalf("GET /documents with limit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", resp.StatusCode)
	}

	var payload struct {
		Documents  []map[string]any `json:"documents"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode documents response: %v", err)
	}

	if len(payload.Documents) != 3 {
		t.Fatalf("expected 3 documents with limit=3, got %d", len(payload.Documents))
	}
	if payload.NextCursor == "" {
		t.Fatal("expected next_cursor when more results exist")
	}
}

func TestDocumentListSearchQuery(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))
	postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"document_id":"search-doc","title":"Search Test Doc"},"content":"content","content_type":"text"}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"document_id":"other-doc","title":"Other Doc"},"content":"content","content_type":"text"}`, http.StatusCreated)

	resp, err := http.Get(h.baseURL + "/docs?q=search")
	if err != nil {
		t.Fatalf("GET /documents with search: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode documents response: %v", err)
	}

	if len(payload.Documents) != 1 {
		t.Fatalf("expected 1 document matching 'search', got %d", len(payload.Documents))
	}
	if payload.Documents[0]["id"] != "search-doc" {
		t.Fatalf("expected search-doc, got %v", payload.Documents[0]["id"])
	}
}

func TestBoardListPaginationLimitParameter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))

	for i := 0; i < 5; i++ {
		postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"id":"board-`+string(rune('a'+i))+`","title":"Board `+string(rune('A'+i))+`","refs":["thread:thread-1"]}}`, http.StatusCreated)
	}

	resp, err := http.Get(h.baseURL + "/boards?limit=3")
	if err != nil {
		t.Fatalf("GET /boards with limit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", resp.StatusCode)
	}

	var payload struct {
		Boards     []map[string]any `json:"boards"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode boards response: %v", err)
	}

	if len(payload.Boards) != 3 {
		t.Fatalf("expected 3 boards with limit=3, got %d", len(payload.Boards))
	}
	if payload.NextCursor == "" {
		t.Fatal("expected next_cursor when more results exist")
	}
}

func TestBoardListSearchQuery(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))
	postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"id":"search-board","title":"Search Test Board","refs":["thread:thread-1"]}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"id":"other-board","title":"Other Board","refs":["thread:thread-1"]}}`, http.StatusCreated)

	resp, err := http.Get(h.baseURL + "/boards?q=search")
	if err != nil {
		t.Fatalf("GET /boards with search: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Boards []map[string]any `json:"boards"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode boards response: %v", err)
	}

	if len(payload.Boards) != 1 {
		t.Fatalf("expected 1 board matching 'search', got %d", len(payload.Boards))
	}
	boardData, ok := payload.Boards[0]["board"].(map[string]any)
	if !ok {
		t.Fatalf("expected board data in response, got %v", payload.Boards[0])
	}
	if boardData["id"] != "search-board" {
		t.Fatalf("expected search-board, got %v", boardData["id"])
	}
}

func TestBackwardCompatibilityWithoutPaginationParams(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-2","display_name":"Actor Two","created_at":"2026-03-04T11:00:00Z"}}`, http.StatusCreated)

	resp, err := http.Get(h.baseURL + "/actors")
	if err != nil {
		t.Fatalf("GET /actors without pagination: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", resp.StatusCode)
	}

	var payload struct {
		Actors     []actors.Actor `json:"actors"`
		NextCursor string         `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode actors response: %v", err)
	}

	if len(payload.Actors) != 2 {
		t.Fatalf("expected all actors without limit, got %d", len(payload.Actors))
	}
}

func TestThreadListPaginationCursorStability(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	for i := 0; i < 5; i++ {
		integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-"+string(rune('a'+i)), "Thread "+string(rune('A'+i))))
	}

	var allThreads []map[string]any
	cursor := ""
	for {
		url := h.baseURL + "/threads?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /threads: %v", err)
		}

		var payload struct {
			Threads    []map[string]any `json:"threads"`
			NextCursor string           `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode threads response: %v", err)
		}
		resp.Body.Close()

		allThreads = append(allThreads, payload.Threads...)

		if payload.NextCursor == "" {
			break
		}
		cursor = payload.NextCursor
	}

	if len(allThreads) != 5 {
		t.Fatalf("expected 5 total threads, got %d", len(allThreads))
	}

	seen := make(map[string]bool)
	for _, thread := range allThreads {
		id, ok := thread["id"].(string)
		if !ok {
			t.Fatalf("thread id is not a string: %v", thread["id"])
		}
		if seen[id] {
			t.Fatalf("duplicate thread in paginated results: %s", id)
		}
		seen[id] = true
	}
}

func TestThreadListInvalidLimit(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))

	testCases := []struct {
		limit   string
		wantErr bool
	}{
		{"0", true},
		{"-1", true},
		{"1001", true},
		{"abc", true},
		{"", false},
		{"1", false},
		{"1000", false},
	}

	for _, tc := range testCases {
		url := h.baseURL + "/threads"
		if tc.limit != "" {
			url += "?limit=" + tc.limit
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /threads with limit=%s: %v", tc.limit, err)
		}
		resp.Body.Close()

		if tc.wantErr && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%s: expected status 400, got %d", tc.limit, resp.StatusCode)
		}
		if !tc.wantErr && resp.StatusCode != http.StatusOK {
			t.Errorf("limit=%s: expected status 200, got %d", tc.limit, resp.StatusCode)
		}
	}
}

func TestDocumentListPaginationCursorStability(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))

	for i := 0; i < 5; i++ {
		postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"document_id":"doc-`+string(rune('a'+i))+`","title":"Doc `+string(rune('A'+i))+`"},"content":"content","content_type":"text"}`, http.StatusCreated)
	}

	var allDocs []map[string]any
	cursor := ""
	for {
		url := h.baseURL + "/docs?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /docs: %v", err)
		}

		var payload struct {
			Documents  []map[string]any `json:"documents"`
			NextCursor string           `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode documents response: %v", err)
		}
		resp.Body.Close()

		allDocs = append(allDocs, payload.Documents...)

		if payload.NextCursor == "" {
			break
		}
		cursor = payload.NextCursor
	}

	if len(allDocs) != 5 {
		t.Fatalf("expected 5 total documents, got %d", len(allDocs))
	}

	seen := make(map[string]bool)
	for _, doc := range allDocs {
		id, ok := doc["id"].(string)
		if !ok {
			t.Fatalf("document id is not a string: %v", doc["id"])
		}
		if seen[id] {
			t.Fatalf("duplicate document in paginated results: %s", id)
		}
		seen[id] = true
	}
}

func TestDocumentListInvalidLimit(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))
	postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"document_id":"doc-1","title":"Doc"},"content":"content","content_type":"text"}`, http.StatusCreated)

	testCases := []struct {
		limit   string
		wantErr bool
	}{
		{"0", true},
		{"-1", true},
		{"1001", true},
		{"abc", true},
		{"", false},
		{"1", false},
		{"1000", false},
	}

	for _, tc := range testCases {
		url := h.baseURL + "/docs"
		if tc.limit != "" {
			url += "?limit=" + tc.limit
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /docs with limit=%s: %v", tc.limit, err)
		}
		resp.Body.Close()

		if tc.wantErr && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%s: expected status 400, got %d", tc.limit, resp.StatusCode)
		}
		if !tc.wantErr && resp.StatusCode != http.StatusOK {
			t.Errorf("limit=%s: expected status 200, got %d", tc.limit, resp.StatusCode)
		}
	}
}

func TestBoardListPaginationCursorStability(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))

	for i := 0; i < 5; i++ {
		postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"id":"board-`+string(rune('a'+i))+`","title":"Board `+string(rune('A'+i))+`","refs":["thread:thread-1"]}}`, http.StatusCreated)
	}

	var allBoards []map[string]any
	cursor := ""
	for {
		url := h.baseURL + "/boards?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /boards: %v", err)
		}

		var payload struct {
			Boards     []map[string]any `json:"boards"`
			NextCursor string           `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode boards response: %v", err)
		}
		resp.Body.Close()

		allBoards = append(allBoards, payload.Boards...)

		if payload.NextCursor == "" {
			break
		}
		cursor = payload.NextCursor
	}

	if len(allBoards) != 5 {
		t.Fatalf("expected 5 total boards, got %d", len(allBoards))
	}

	seen := make(map[string]bool)
	for _, boardItem := range allBoards {
		board, ok := boardItem["board"].(map[string]any)
		if !ok {
			t.Fatalf("board data is not a map: %v", boardItem["board"])
		}
		id, ok := board["id"].(string)
		if !ok {
			t.Fatalf("board id is not a string: %v", board["id"])
		}
		if seen[id] {
			t.Fatalf("duplicate board in paginated results: %s", id)
		}
		seen[id] = true
	}
}

func TestBoardListInvalidLimit(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread"))
	postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"id":"board-1","title":"Board","refs":["thread:thread-1"]}}`, http.StatusCreated)

	testCases := []struct {
		limit   string
		wantErr bool
	}{
		{"0", true},
		{"-1", true},
		{"1001", true},
		{"abc", true},
		{"", false},
		{"1", false},
		{"1000", false},
	}

	for _, tc := range testCases {
		url := h.baseURL + "/boards"
		if tc.limit != "" {
			url += "?limit=" + tc.limit
		}

		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET /boards with limit=%s: %v", tc.limit, err)
		}
		resp.Body.Close()

		if tc.wantErr && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit=%s: expected status 400, got %d", tc.limit, resp.StatusCode)
		}
		if !tc.wantErr && resp.StatusCode != http.StatusOK {
			t.Errorf("limit=%s: expected status 200, got %d", tc.limit, resp.StatusCode)
		}
	}
}

func TestPaginationInvalidCursor(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	integrationSeedThread(t, h, "actor-1", paginationTestThread("thread-1", "Thread One"))
	postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-1","document":{"document_id":"doc-1","title":"Doc"},"content":"content","content_type":"text"}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"id":"board-1","title":"Board","refs":["thread:thread-1"]}}`, http.StatusCreated)

	testCases := []struct {
		name string
		url  string
	}{
		{name: "actors", url: h.baseURL + "/actors?limit=2&cursor=not-base64"},
		{name: "threads", url: h.baseURL + "/threads?limit=2&cursor=not-base64"},
		{name: "documents", url: h.baseURL + "/docs?limit=2&cursor=not-base64"},
		{name: "boards", url: h.baseURL + "/boards?limit=2&cursor=not-base64"},
		{name: "actors-semantic", url: h.baseURL + "/actors?limit=2&cursor=" + base64.StdEncoding.EncodeToString([]byte("offset:0"))},
		{name: "threads-semantic", url: h.baseURL + "/threads?limit=2&cursor=" + base64.StdEncoding.EncodeToString([]byte("offset:-1"))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(tc.url)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.url, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", resp.StatusCode)
			}
			assertErrorCode(t, resp, "invalid_request")
		})
	}
}
