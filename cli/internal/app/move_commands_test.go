package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

func TestParseMoveCommand(t *testing.T) {
	parsed, err := parseMoveCommand([]string{"card", "card:launch", "--to", "archive", "--dry-run"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.kind != "card" || parsed.ref != "card:launch" || parsed.target != "archive" || !parsed.dryRun {
		t.Fatalf("unexpected parsed command: %#v", parsed)
	}
	if _, err := parseMoveCommand([]string{"topic", "topic:launch"}); err == nil {
		t.Fatal("missing --to was accepted")
	}
	if _, err := parseMoveCommand([]string{"card", "https://example.test/card", "--to", "archive"}); err == nil {
		t.Fatal("URL was accepted as a resource ref")
	}
}

func TestMoveTopicDryRunAndResumesPartialCopy(t *testing.T) {
	ctx := context.Background()
	source := newMoveTestWorkspace(t, true)
	destination := newMoveTestWorkspace(t, false)
	sourceServer := httptest.NewServer(source)
	t.Cleanup(sourceServer.Close)
	destinationServer := httptest.NewServer(destination)
	t.Cleanup(destinationServer.Close)
	sourceCfg := moveTestConfig(sourceServer.URL, "source-token")
	destCfg := moveTestConfig(destinationServer.URL, "destination-token")
	app := New()

	preview, err := app.moveTopic(ctx, sourceCfg, destCfg, "topic:launch", true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if preview.Data.(map[string]any)["status"] != "dry_run" || source.mutationCount() != 0 || destination.mutationCount() != 0 {
		t.Fatalf("dry run mutated a workspace or omitted preview status: result=%#v source_writes=%d destination_writes=%d", preview.Data, source.mutationCount(), destination.mutationCount())
	}
	joinedActions := fmt.Sprint(asMap(preview.Data)["actions"])
	for _, expected := range []string{"document", "board", "card", "archive", "tombstone", "resync"} {
		if !strings.Contains(joinedActions, expected) {
			t.Fatalf("dry run action list omitted %q: %#v", expected, asMap(preview.Data)["actions"])
		}
	}

	destination.failNextCardCreate = true
	if _, err := app.moveTopic(ctx, sourceCfg, destCfg, "topic:launch", false); err == nil {
		t.Fatal("simulated destination failure was not returned")
	}
	if moveState(source.topic) == "archived" || destination.createCount["topic"] != 1 || destination.createCount["document"] != 1 || destination.createCount["board"] != 1 || destination.createCount["card"] != 0 {
		t.Fatalf("partial failure advanced source lifecycle or lost resumable writes: topic=%#v target_counts=%#v", source.topic, destination.createCount)
	}
	writesBeforePreview := destination.mutationCount()
	resumePreview, err := app.moveTopic(ctx, sourceCfg, destCfg, "topic:launch", true)
	if err != nil {
		t.Fatalf("dry run after partial copy: %v", err)
	}
	boardReused := false
	if actions, ok := asMap(resumePreview.Data)["actions"].([]map[string]any); ok {
		for _, action := range actions {
			if action["kind"] == "board" && action["action"] == "reuse" {
				boardReused = true
			}
		}
	}
	if destination.mutationCount() != writesBeforePreview || !boardReused {
		t.Fatalf("resumed dry run failed to reuse the cited board or performed writes: result=%#v writes=%d/%d", resumePreview.Data, writesBeforePreview, destination.mutationCount())
	}

	result, err := app.moveTopic(ctx, sourceCfg, destCfg, "topic:launch", false)
	if err != nil {
		t.Fatalf("resume move: %v", err)
	}
	if asMap(result.Data)["status"] != "moved" {
		t.Fatalf("unexpected result: %#v", result.Data)
	}
	if destination.createCount["topic"] != 1 || destination.createCount["document"] != 1 || destination.createCount["board"] != 1 || destination.createCount["card"] != 2 {
		t.Fatalf("resume duplicated or dropped destination resources: %#v", destination.createCount)
	}
	if moveState(source.topic) != "archived" || moveState(asMap(source.documents["document:spec"]["document"])) != "archived" || moveState(source.works["card:curated"]) != "archived" || moveState(source.works["card:linked"]) != "archived" {
		t.Fatalf("source topic, docs and cards were not archived: topic=%#v docs=%#v work=%#v", source.topic, source.documents, source.works)
	}
	if moveState(source.boards["board:launch-board"]) != "archived" {
		t.Fatalf("source board was not archived: %#v", source.boards)
	}

	topicMove := asMap(destination.topic["workspace_move"])
	if topicMove["move_id"] == "" || topicMove["status"] != "complete" || topicMove["source_ref"] != "topic:launch" || topicMove["source_url"] == "" {
		t.Fatalf("destination topic marker omitted source citation: %#v", topicMove)
	}
	documentRef := moveResourceRef("document", moveDeterministicUUID(anyString(topicMove["move_id"]), "document", "document:spec"))
	boardRef := moveResourceRef("board", moveDeterministicUUID(anyString(topicMove["move_id"]), "board", "board:launch-board"))
	if !containsString(moveStringList(destination.topic["document_refs"]), documentRef) || !containsString(moveStringList(destination.topic["board_refs"]), boardRef) {
		t.Fatalf("topic refs were not rewritten: topic=%#v", destination.topic)
	}
	movedDocument := asMap(destination.documents[documentRef]["document"])
	if movedDocument["thread_id"] != moveDeterministicUUID(anyString(topicMove["move_id"]), "document_thread", "document:spec") || !containsString(moveStringList(movedDocument["refs"]), moveResourceRef("topic", moveDeterministicUUID(anyString(topicMove["move_id"]), "topic", "topic:launch"))) {
		t.Fatalf("document thread/ref identities were not rewritten: %#v", movedDocument)
	}
	movedBoard := destination.boards[boardRef]
	if movedBoard["thread_id"] != moveDeterministicUUID(anyString(topicMove["move_id"]), "board_thread", "board:launch-board") {
		t.Fatalf("board thread identity was not rewritten: %#v", movedBoard)
	}
	curated := destination.works[moveResourceRef("card", moveDeterministicUUID(anyString(topicMove["move_id"]), "card", "card:curated"))]
	linked := findFakeWorkByExternalIdentity(destination, "github", "gh-main", "issue-42")
	if curated == nil || linked == nil {
		t.Fatalf("missing curated/source-backed destination cards: %#v", destination.works)
	}
	curatedTombstone := asMap(asMap(source.works["card:curated"]["workspace_move"])["tombstone"])
	if asMap(source.works["card:curated"]["workspace_move"])["source_action"] != "tombstone" || curatedTombstone["destination_ref"] != moveFieldString(curated, "ref") || !strings.Contains(anyString(curatedTombstone["destination_url"]), moveRefID(moveFieldString(curated, "ref"))) {
		t.Fatalf("curated source card omitted its destination tombstone link: %#v", source.works["card:curated"]["workspace_move"])
	}
	if asMap(source.works["card:linked"]["workspace_move"])["source_action"] != "archive" {
		t.Fatalf("source-backed card did not record archive lifecycle: %#v", source.works["card:linked"]["workspace_move"])
	}
	if moveFieldString(curated, "topic_ref") != moveResourceRef("topic", moveDeterministicUUID(anyString(topicMove["move_id"]), "topic", "topic:launch")) || moveFieldString(curated, "document_ref") != documentRef || !containsString(moveStringList(curated["related_refs"]), boardRef) {
		t.Fatalf("moved card refs were not rewritten: %#v", curated)
	}
	linkedSource := asMap(linked["source"])
	if linkedSource["authority"] != "github" || linkedSource["connection_id"] != "gh-main" || linkedSource["native_id"] != "issue-42" {
		t.Fatalf("source-backed card lost its source identity: %#v", linkedSource)
	}
	assertMoveCitationInWrites(t, destination.writes)
}

func TestMoveCardCopiesCuratedAndResyncsSourceBacked(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source map[string]any
		want   string
	}{
		{name: "curated", source: map[string]any{"authority": "nexus"}, want: "tombstone"},
		{name: "source backed", source: map[string]any{"authority": "github", "connection_id": "gh-main", "native_id": "issue-7"}, want: "archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := newMoveTestWorkspace(t, false)
			destination := newMoveTestWorkspace(t, false)
			sourceServer := httptest.NewServer(source)
			t.Cleanup(sourceServer.Close)
			destinationServer := httptest.NewServer(destination)
			t.Cleanup(destinationServer.Close)
			source.workspacesSetupSingleCard("card:source-card", tc.source)
			app := New()
			result, err := app.moveCard(context.Background(), moveTestConfig(sourceServer.URL, "source-token"), moveTestConfig(destinationServer.URL, "destination-token"), "card:source-card", false)
			if err != nil {
				t.Fatalf("move card: %v", err)
			}
			if asMap(result.Data)["status"] != "moved" || moveState(source.works["card:source-card"]) != "archived" || destination.createCount["card"] != 1 {
				t.Fatalf("card move did not finish: result=%#v source=%#v destination=%#v", result.Data, source.works, destination.works)
			}
			if !strings.Contains(fmt.Sprint(asMap(result.Data)["actions"]), tc.want) {
				t.Fatalf("expected %s source lifecycle action: %#v", tc.want, result.Data)
			}
			marker := asMap(source.works["card:source-card"]["workspace_move"])
			if marker["source_action"] != tc.want {
				t.Fatalf("source marker omitted %s action: %#v", tc.want, marker)
			}
			if tc.want == "tombstone" {
				tombstone := asMap(marker["tombstone"])
				if tombstone["destination_ref"] != moveFieldString(destination.works[moveFieldString(marker, "destination_ref")], "ref") || tombstone["destination_url"] == "" {
					t.Fatalf("source tombstone omitted destination link: %#v", marker)
				}
			}
		})
	}
}

func moveTestConfig(baseURL, token string) config.Resolved {
	return config.Resolved{BaseURL: baseURL, AccessToken: token, Timeout: 2 * time.Second, Sources: map[string]string{"base_url": "flag:--base-url"}}
}

type moveFakeWorkspace struct {
	mu                  sync.Mutex
	isSource            bool
	topic               map[string]any
	workspace           map[string]any
	documents           map[string]map[string]any
	boards              map[string]map[string]any
	works               map[string]map[string]any
	writes              []map[string]any
	createCount         map[string]int
	failNextBoardCreate bool
	failNextCardCreate  bool
	updatedAtSequence   int
}

func newMoveTestWorkspace(t *testing.T, source bool) *moveFakeWorkspace {
	t.Helper()
	fake := &moveFakeWorkspace{
		isSource:  source,
		documents: map[string]map[string]any{}, boards: map[string]map[string]any{}, works: map[string]map[string]any{},
		createCount: map[string]int{},
	}
	if !source {
		return fake
	}
	updatedAt := "2026-10-01T00:00:00Z"
	fake.topic = map[string]any{
		"id": "topic-source", "ref": "topic:launch", "handle": "launch", "thread_id": "thread-source",
		"state": "active", "title": "Launch", "summary": "Coordinate launch", "updated_at": updatedAt,
		"owner_refs": []any{"actor:source-owner"}, "document_refs": []any{"document:spec"},
		"board_refs": []any{"board:launch-board"}, "related_refs": []any{"card:curated", "card:linked"},
		"provenance": map[string]any{"sources": []any{"https://source.example/launch"}},
	}
	doc := map[string]any{
		"id": "doc-source", "ref": "document:spec", "handle": "spec", "thread_id": "thread-doc-source", "state": "active", "title": "Launch spec",
		"summary": "Release requirements", "source": "https://source.example/spec", "tags": []any{"spec"}, "hosts": []any{},
		"subject_ref": "thread:thread-doc-source", "refs": []any{"topic:launch", "card:curated"},
		"provenance": map[string]any{"sources": []any{"topic:launch"}},
	}
	revision := map[string]any{
		"content_type": "text", "content": "Keep launch behavior stable.",
		"refs":     []any{"topic:launch", "card:curated"},
		"artifact": map[string]any{"content_type": "text"},
	}
	fake.documents["document:spec"] = map[string]any{"document": doc, "revision": revision}
	board := map[string]any{
		"id": "board-source", "ref": "board:launch-board", "handle": "launch-board", "thread_id": "thread-board-source", "state": "active",
		"title": "Launch work", "summary": "Launch tasks", "primary_topic_ref": "topic:launch",
		"document_refs": []any{"document:spec"}, "pinned_refs": []any{"card:curated"},
		"column_schema": map[string]any{"columns": []any{map[string]any{"key": "backlog", "title": "Backlog"}}},
		"provenance":    map[string]any{"sources": []any{"topic:launch"}},
	}
	fake.boards["board:launch-board"] = board
	curated := map[string]any{
		"id": "card-curated-source", "ref": "card:curated", "handle": "curated", "state": "active", "version": 1,
		"board_ref": "board:launch-board", "title": "Prepare announcement", "summary": "Draft launch note",
		"definition_of_done": []any{"Review copy"}, "phase": "in_progress", "priority": "p1", "risk": "medium",
		"source": map[string]any{"authority": "nexus"}, "topic_ref": "topic:launch", "document_ref": "document:spec",
		"related_refs": []any{"board:launch-board"}, "plan": map[string]any{"steps": []any{"draft", "review"}},
	}
	linked := map[string]any{
		"id": "card-linked-source", "ref": "card:linked", "handle": "linked", "state": "active", "version": 1,
		"board_ref": "board:launch-board", "title": "Fix release blocker", "summary": "A linked issue",
		"definition_of_done": []any{}, "phase": "ready", "priority": "p2",
		"source":    map[string]any{"authority": "github", "connection_id": "gh-main", "native_id": "issue-42", "url": "https://github.example/issues/42"},
		"topic_ref": "topic:launch", "document_ref": "document:spec", "related_refs": []any{"board:launch-board"},
	}
	fake.works["card:curated"] = curated
	fake.works["card:linked"] = linked
	fake.workspace = map[string]any{"documents": []any{doc}, "boards": []any{board}, "cards": []any{curated, linked}}
	return fake
}

func (f *moveFakeWorkspace) workspacesSetupSingleCard(ref string, source map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	work := map[string]any{
		"id": "source-card", "ref": ref, "handle": strings.TrimPrefix(ref, "card:"), "state": "active", "version": 1,
		"board_ref": "board:tasks", "title": "Move me", "summary": "Card summary", "phase": "ready",
		"definition_of_done": []any{"Verify"}, "priority": "p1", "source": source,
		"related_refs": []any{}, "plan": map[string]any{"note": "Keep this"},
	}
	f.works[ref] = work
}

func (f *moveFakeWorkspace) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	body := map[string]any{}
	if r.Body != nil && r.Method != http.MethodGet {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, map[string]any{"method": r.Method, "path": r.URL.Path, "body": body})
	}
	respond := func(status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	notFound := func() {
		respond(http.StatusNotFound, map[string]any{"error": map[string]any{"code": "not_found", "message": "not found"}})
	}
	conflict := func() {
		respond(http.StatusConflict, map[string]any{"error": map[string]any{"code": "conflict", "message": "conflict"}})
	}
	if len(segments) == 1 && segments[0] == "work" {
		if r.Method == http.MethodGet {
			works := []any{}
			for _, work := range f.works {
				works = append(works, work)
			}
			respond(http.StatusOK, map[string]any{"work": works, "next_cursor": ""})
			return
		}
		if r.Method == http.MethodPost {
			if f.failNextCardCreate {
				f.failNextCardCreate = false
				respond(http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": "internal_error", "message": "simulated partial card failure"}})
				return
			}
			input := body
			source := asMap(input["source"])
			for _, existing := range f.works {
				identity := asMap(existing["source"])
				if moveFieldString(source, "authority") != "" && moveFieldString(source, "authority") != "nexus" && moveFieldString(source, "authority") == moveFieldString(identity, "authority") && moveFieldString(source, "connection_id") == moveFieldString(identity, "connection_id") && moveFieldString(source, "native_id") == moveFieldString(identity, "native_id") {
					respond(http.StatusCreated, map[string]any{"work": existing})
					return
				}
			}
			id := moveFieldString(input, "id")
			if id == "" {
				id = fmt.Sprintf("created-%d", f.createCount["card"]+1)
			}
			ref := moveResourceRef("card", id)
			if existing := fakeFindResource(f.works, ref); existing != nil {
				marker := asMap(existing["workspace_move"])
				if marker["move_id"] == asMap(input["workspace_move"])["move_id"] {
					respond(http.StatusCreated, map[string]any{"work": existing})
				} else {
					conflict()
				}
				return
			}
			work := map[string]any{}
			for key, value := range input {
				work[key] = value
			}
			work["id"], work["ref"], work["handle"], work["state"], work["version"] = id, ref, id, "active", 1
			f.works[ref] = work
			f.createCount["card"]++
			respond(http.StatusCreated, map[string]any{"work": work})
			return
		}
	}
	if len(segments) == 2 && segments[0] == "work" {
		ref := segments[1]
		work := fakeFindResource(f.works, ref)
		if work == nil {
			notFound()
			return
		}
		if r.Method == http.MethodGet {
			respond(http.StatusOK, map[string]any{"work": work})
			return
		}
		if r.Method == http.MethodPatch {
			if moveTestInt(work["version"]) != moveTestInt(body["if_version"]) {
				conflict()
				return
			}
			for key, value := range asMap(body["patch"]) {
				work[key] = value
			}
			work["version"] = moveTestInt(work["version"]) + 1
			respond(http.StatusOK, map[string]any{"work": work})
			return
		}
	}
	if len(segments) == 3 && segments[0] == "cards" && segments[2] == "archive" {
		work := fakeFindResource(f.works, segments[1])
		if work == nil {
			notFound()
			return
		}
		work["state"] = "archived"
		respond(http.StatusOK, map[string]any{"card": work})
		return
	}
	if len(segments) == 1 && segments[0] == "topics" && r.Method == http.MethodPost {
		topic := asMap(body["topic"])
		id := moveFieldString(topic, "id")
		ref := moveResourceRef("topic", id)
		if existing := fakeFindResource(map[string]map[string]any{"current": f.topic}, ref); existing != nil {
			respond(http.StatusCreated, map[string]any{"topic": existing})
			return
		}
		f.updatedAtSequence++
		created := map[string]any{}
		for key, value := range topic {
			created[key] = value
		}
		created["id"], created["ref"], created["handle"], created["state"] = id, ref, id, "active"
		created["updated_at"] = "2026-10-01T00:00:00Z"
		f.topic = created
		f.createCount["topic"]++
		respond(http.StatusCreated, map[string]any{"topic": created})
		return
	}
	if len(segments) == 2 && segments[0] == "topics" {
		if r.Method == http.MethodGet && len(segments) == 2 {
			if f.topic == nil || fakeFindResource(map[string]map[string]any{"current": f.topic}, segments[1]) == nil {
				notFound()
				return
			}
			respond(http.StatusOK, map[string]any{"topic": f.topic})
			return
		}
		if r.Method == http.MethodPatch {
			if f.topic == nil || fakeFindResource(map[string]map[string]any{"current": f.topic}, segments[1]) == nil {
				notFound()
				return
			}
			for key, value := range asMap(body["patch"]) {
				f.topic[key] = value
			}
			f.updatedAtSequence++
			f.topic["updated_at"] = fmt.Sprintf("2026-10-01T00:00:%02dZ", f.updatedAtSequence)
			respond(http.StatusOK, map[string]any{"topic": f.topic})
			return
		}
	}
	if len(segments) == 3 && segments[0] == "topics" && segments[2] == "workspace" {
		workspace := map[string]any{}
		for key, value := range f.workspace {
			workspace[key] = value
		}
		if f.topic != nil {
			workspace["topic"] = f.topic
		}
		respond(http.StatusOK, workspace)
		return
	}
	if len(segments) == 3 && segments[0] == "topics" && segments[2] == "archive" {
		if f.topic == nil {
			notFound()
			return
		}
		f.topic["state"] = "archived"
		respond(http.StatusOK, map[string]any{"topic": f.topic})
		return
	}
	if len(segments) == 1 && segments[0] == "docs" && r.Method == http.MethodPost {
		docInput := asMap(body["document"])
		id := moveFieldString(docInput, "document_id")
		ref := moveResourceRef("document", id)
		if existing := fakeFindResource(f.documents, ref); existing != nil {
			respond(http.StatusCreated, existing)
			return
		}
		doc := map[string]any{}
		for key, value := range docInput {
			doc[key] = value
		}
		doc["id"], doc["ref"], doc["handle"], doc["state"] = id, ref, id, "active"
		content := body["content"]
		if body["content_base64"] != nil {
			content = nil
		}
		revision := map[string]any{"content": content, "content_base64": body["content_base64"], "content_type": body["content_type"], "refs": body["refs"], "artifact": map[string]any{"workspace_move": docInput["workspace_move"], "content_type": body["content_type"]}}
		documentResponse := map[string]any{"document": doc, "revision": revision}
		f.documents[ref] = documentResponse
		f.createCount["document"]++
		respond(http.StatusCreated, documentResponse)
		return
	}
	if len(segments) == 2 && segments[0] == "docs" {
		doc := fakeFindResource(f.documents, segments[1])
		if doc == nil {
			notFound()
			return
		}
		respond(http.StatusOK, doc)
		return
	}
	if len(segments) == 3 && segments[0] == "docs" && segments[2] == "archive" {
		doc := fakeFindResource(f.documents, segments[1])
		if doc == nil {
			notFound()
			return
		}
		asMap(doc["document"])["state"] = "archived"
		respond(http.StatusOK, doc)
		return
	}
	if len(segments) == 1 && segments[0] == "boards" && r.Method == http.MethodPost {
		if f.failNextBoardCreate {
			f.failNextBoardCreate = false
			respond(http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": "internal_error", "message": "simulated partial failure"}})
			return
		}
		boardInput := asMap(body["board"])
		id := moveFieldString(boardInput, "id")
		ref := moveResourceRef("board", id)
		if existing := fakeFindResource(f.boards, ref); existing != nil {
			respond(http.StatusCreated, map[string]any{"board": existing})
			return
		}
		board := map[string]any{}
		for key, value := range boardInput {
			board[key] = value
		}
		board["id"], board["ref"], board["handle"], board["state"] = id, ref, id, "active"
		f.boards[ref] = board
		f.createCount["board"]++
		respond(http.StatusCreated, map[string]any{"board": board})
		return
	}
	if len(segments) == 2 && segments[0] == "boards" {
		board := fakeFindResource(f.boards, segments[1])
		if board == nil {
			notFound()
			return
		}
		respond(http.StatusOK, map[string]any{"board": board})
		return
	}
	if len(segments) == 3 && segments[0] == "boards" && segments[2] == "archive" {
		board := fakeFindResource(f.boards, segments[1])
		if board == nil {
			notFound()
			return
		}
		board["state"] = "archived"
		respond(http.StatusOK, map[string]any{"board": board})
		return
	}
	if len(segments) == 3 && segments[0] == "threads" && segments[2] == "timeline" {
		events := []any{}
		for _, board := range f.boards {
			if moveFieldString(board, "thread_id") == segments[1] {
				events = append(events, map[string]any{"type": "board_created", "payload": map[string]any{"workspace_move": board["workspace_move"]}})
			}
		}
		respond(http.StatusOK, map[string]any{"events": events})
		return
	}
	notFound()
}

func (f *moveFakeWorkspace) mutationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func fakeFindResource(resources map[string]map[string]any, requested string) map[string]any {
	requestedID := moveRefID(requested)
	for _, resource := range resources {
		candidate := resource
		if document := asMap(resource["document"]); document != nil {
			candidate = document
		}
		for _, key := range []string{"ref", "id", "handle"} {
			value := moveFieldString(candidate, key)
			if value == requested || value == requestedID || moveRefID(value) == requestedID {
				return resource
			}
		}
	}
	return nil
}

func findFakeWorkByExternalIdentity(f *moveFakeWorkspace, authority, connectionID, nativeID string) map[string]any {
	for _, work := range f.works {
		source := asMap(work["source"])
		if moveFieldString(source, "authority") == authority && moveFieldString(source, "connection_id") == connectionID && moveFieldString(source, "native_id") == nativeID {
			return work
		}
	}
	return nil
}

func assertMoveCitationInWrites(t *testing.T, writes []map[string]any) {
	t.Helper()
	found := map[string]bool{}
	for _, write := range writes {
		if write["method"] != http.MethodPost {
			continue
		}
		path := strings.TrimRight(anyString(write["path"]), "/")
		if path != "/topics" && path != "/docs" && path != "/boards" && path != "/work" {
			continue
		}
		body := asMap(write["body"])
		resource := body
		switch path {
		case "/topics":
			resource = asMap(body["topic"])
		case "/docs":
			resource = asMap(body["document"])
		case "/boards":
			resource = asMap(body["board"])
		}
		marker := asMap(resource["workspace_move"])
		if marker["move_id"] != "" && marker["source_ref"] != "" && marker["source_url"] != "" {
			found[path] = true
		}
	}
	for _, path := range []string{"/topics", "/docs", "/boards", "/work"} {
		if !found[path] {
			t.Errorf("first destination event payload for %s omitted the source citation", path)
		}
	}
}

func moveTestInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}
