package primitives

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWatchRollupData(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	create := func(title string, fields map[string]any) map[string]any {
		t.Helper()
		if fields == nil {
			fields = map[string]any{}
		}
		fields["title"] = title
		w, e := s.CreateWork(ctx, "writer", "", fields)
		if e != nil {
			t.Fatal(e)
		}
		return w
	}
	root := create("Initiative", map[string]any{"summary": "Deliver a usable board\nDetails stay out of the outcome", "initiative": true})
	stream := create("P76: storage", map[string]any{"under": root["ref"], "owner": "actor:writer"})
	leaf := create("fs-p3: measure", map[string]any{"under": stream["ref"]})
	done := create("A1: shipped", map[string]any{"under": stream["ref"]})
	for _, pair := range []struct {
		card map[string]any
		at   string
	}{{leaf, "2000-01-01"}, {stream, "2099-01-01"}} {
		_, err = s.AppendEvent(ctx, "writer", map[string]any{"type": "message_posted", "thread_id": pair.card["thread_id"], "refs": []string{workString(pair.card["ref"])}, "payload": map[string]any{"next": map[string]any{"at": pair.at, "what": "Ship"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the canonical terminal event; unrelated updates never date completion.
	if _, err = ws.DB().Exec(`UPDATE cards SET column_key='done',updated_at='2099-01-01T00:00:00Z' WHERE id=?`, done["id"]); err != nil {
		t.Fatal(err)
	}
	terminal, err := s.AppendEvent(ctx, "writer", map[string]any{"type": "card_moved", "thread_id": done["thread_id"], "refs": []string{workString(done["ref"])}, "payload": map[string]any{"card_id": done["id"], "column_key": "done", "from_column_key": "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		drained, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	readCtx := WithRequestAccessScope(ctx, AccessScope{ActorID: "viewer"})
	if err = s.EnrichWorkHierarchy(readCtx, []map[string]any{root}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := root["rollup"].(*WorkRollup)
	if r.Total != 1 || len(r.Workstreams) != 1 || r.Children[0].Rollup == nil {
		t.Fatalf("root: %+v", r)
	}
	w := r.Workstreams[0]
	if w.Owner == nil || *w.Owner != "actor:writer" || w.ShortCode != "p76" {
		t.Fatalf("stream: %+v", w)
	}
	if w.Rollup.Total != 2 || w.Rollup.ByState["done"] != 1 || w.Rollup.ByState["backlog"] != 1 || w.Rollup.PastPromise != 1 || w.Rollup.EarliestPromiseAt == nil || !strings.HasPrefix(*w.Rollup.EarliestPromiseAt, "2000-01-01") {
		t.Fatalf("stream rollup: %+v", w.Rollup)
	}
	for _, c := range w.Rollup.Children {
		if c.Ref == done["ref"] {
			if c.DoneAt == nil || *c.DoneAt != workString(terminal["ts"]) {
				t.Fatalf("completion: %+v event=%v", c, terminal)
			}
		} else if c.DoneAt != nil {
			t.Fatalf("open child dated complete: %+v", c)
		}
	}
	// RFC3339 fractional seconds must compare as times, not lexical strings.
	earlier := create("Exact-second promise", map[string]any{"under": stream["ref"]})
	for _, pair := range []struct {
		card map[string]any
		at   string
	}{{leaf, "2000-01-01T00:00:00.1Z"}, {earlier, "2000-01-01T00:00:00Z"}} {
		if _, err = s.AppendEvent(ctx, "writer", map[string]any{"type": "message_posted", "thread_id": pair.card["thread_id"], "refs": []string{workString(pair.card["ref"])}, "payload": map[string]any{"next": map[string]any{"at": pair.at, "what": "Ship"}}}); err != nil {
			t.Fatal(err)
		}
	}
	for drained := false; !drained; {
		drained, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = s.EnrichWorkHierarchy(readCtx, []map[string]any{stream}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if at := stream["rollup"].(*WorkRollup).EarliestPromiseAt; at == nil || *at != "2000-01-01T00:00:00Z" {
		t.Fatalf("earliest fractional promise: %v", at)
	}
	for _, card := range []map[string]any{leaf, earlier} {
		if _, err = s.ArchiveBoardCard(ctx, "writer", "", workString(card["id"]), RemoveBoardCardInput{}); err != nil {
			t.Fatal(err)
		}
	}
	for drained := false; !drained; {
		drained, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = s.EnrichWorkHierarchy(readCtx, []map[string]any{stream}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if rollup := stream["rollup"].(*WorkRollup); rollup.PastPromise != 0 || rollup.EarliestPromiseAt != nil {
		t.Fatalf("archived children retained open promises: %+v", rollup)
	}
	brief, err := s.Brief(readCtx, BriefReader{ActorID: "viewer", Kind: "human"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	items := brief["initiatives"].([]map[string]any)
	if len(items) != 1 || items[0]["outcome"] != "Deliver a usable board" || items[0]["proposed"] != true {
		t.Fatalf("brief: %v", items)
	}
	goal, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Measured outcome", "summary": "Fallback", "goal": map[string]any{"metric": "score", "direction": "up", "label": "Raise the score"}})
	if err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		drained, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	brief, err = s.Brief(readCtx, BriefReader{ActorID: "viewer", Kind: "human"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	foundGoal := false
	for _, item := range brief["loops"].([]map[string]any) {
		if item["ref"] == goal["ref"] {
			foundGoal = item["outcome"] == "Raise the score"
		}
	}
	if !foundGoal {
		t.Fatalf("goal outcome missing: %v", brief["loops"])
	}
	// A private grandchild/plan cannot influence visible counts or labels.
	b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(b["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	// An unreadable latest terminal event may supply no timestamp. Its internal
	// routing identity must never become a result or fall back to an older date.
	if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "card_moved", "thread_id": done["thread_id"], "refs": []string{workString(done["ref"]), "board:" + workString(b["id"])}, "payload": map[string]any{"card_id": done["id"], "column_key": "done", "from_column_key": "ready"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(readCtx, []map[string]any{stream}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, c := range stream["rollup"].(*WorkRollup).Children {
		if c.Ref == done["ref"] && c.DoneAt != nil {
			t.Fatalf("hidden terminal event supplied a date: %+v", c)
		}
	}
	if err = s.EnrichWorkHierarchy(readCtx, []map[string]any{root}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	beforeHidden, _ := json.Marshal(root["rollup"])
	hidden, err := s.CreateWork(ctx, "owner", workString(b["id"]), map[string]any{"title": "Secret P99", "owner": "actor:owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GroupWork(ctx, "owner", workString(hidden["ref"]), workString(stream["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(readCtx, []map[string]any{root}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(root["rollup"])
	if strings.Contains(string(raw), "Secret") || strings.Contains(string(raw), workString(hidden["ref"])) || strings.Contains(string(raw), "actor:owner") {
		t.Fatalf("hidden data: %s", raw)
	}
	if string(raw) != string(beforeHidden) {
		t.Fatalf("hidden child changed visible rollup: before=%s after=%s", beforeHidden, raw)
	}
}

func TestWatchUngroupedCreatorAndActions(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err = ws.DB().Exec(`INSERT INTO actors(id,display_name,created_at) VALUES('writer','Alex','now')`); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Ungrouped"})
	if err != nil {
		t.Fatal(err)
	}
	readCtx := WithRequestAccessScope(ctx, AccessScope{ActorID: "viewer"})
	items, more, err := s.UngroupedWork(readCtx, 20)
	if err != nil || more || len(items) != 1 {
		t.Fatalf("%v %v %v", items, more, err)
	}
	if workMap(items[0]["created_by"])["display"] != "Alex" {
		t.Fatal(items)
	}
	actions := items[0]["next_actions"].([]map[string]any)
	if len(actions) != 2 || actions[0]["command_id"] != "work.move" || actions[1]["requires_human"] != true || actions[1]["argv"] != nil {
		t.Fatal(actions)
	}
	argv := actions[0]["argv"].([]string)
	if strings.Join(argv[:5], " ") != "anx work move "+workString(w["ref"])+" --root" {
		t.Fatalf("invalid promotion command: %v", argv)
	}
	if _, err = s.MoveWork(ctx, "writer", workString(w["ref"]), MoveWorkInput{Root: true, Reason: "Independent outcome"}); err != nil {
		t.Fatal(err)
	}
	items, _, err = s.UngroupedWork(readCtx, 20)
	if err != nil || len(items) != 0 {
		t.Fatalf("intent not retained: %v %v", items, err)
	}
	current, err := s.GetWork(ctx, workString(w["ref"]))
	if err != nil || current["is_initiative"] != true {
		t.Fatalf("%v %v", current, err)
	}
	if _, err = s.MoveWork(ctx, "writer", workString(w["ref"]), MoveWorkInput{Unsorted: true, Reason: "Needs a home"}); err != nil {
		t.Fatal(err)
	}
	items, _, err = s.UngroupedWork(readCtx, 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("intent not reversible: %v %v", items, err)
	}
	if err = ValidateWorkAnnotations(map[string]any{"top_level": "yes"}); err == nil {
		t.Fatal("accepted non-boolean intent")
	}
	b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private creator context"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(b["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE actors SET display_name=? WHERE id='writer'`, "Private board:"+workString(b["id"])); err != nil {
		t.Fatal(err)
	}
	items, _, err = s.UngroupedWork(readCtx, 20)
	if err != nil || len(items) != 1 || items[0]["created_by"] != nil {
		t.Fatalf("private profile exposed: %v %v", items, err)
	}
}

func TestWatchOutcomeAndCodes(t *testing.T) {
	if got := briefOutcome(strings.Repeat("界", 300) + "\nsecond"); len([]rune(got)) != 280 {
		t.Fatalf("outcome length: %d", len([]rune(got)))
	}
	for _, c := range []struct{ handle, title, want string }{{"fs-p3", "P76: other", "fs-p3"}, {"plain", "P76: task", "p76"}, {"plain", "Ordinary prose", ""}, {"", "Cost 2/5: vendor spend", "cost"}} {
		if got := WorkShortCode(c.handle, c.title); got != c.want {
			t.Fatalf("%+v: %q", c, got)
		}
	}
}

func TestWatchStoredParentDoesNotFallBackToVisibleParent(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(b["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	older, err := s.CreateWork(ctx, "owner", workString(b["id"]), map[string]any{"id": "a-parent", "title": "Private older parent"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateWork(ctx, "owner", workString(publicBoard["id"]), map[string]any{"id": "b-parent", "title": "Public parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public child", "under": public["ref"]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GroupWork(ctx, "owner", workString(child["ref"]), workString(older["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	// Stored parentage does not fall back to another visible parent.
	if _, err = ws.DB().Exec(`UPDATE cards SET created_at='2000-01-01T00:00:00Z' WHERE id IN ('a-parent','b-parent')`); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"owner", "stranger"} {
		readCtx := WithRequestAccessScope(ctx, AccessScope{ActorID: actor})
		items := []map[string]any{{"id": public["id"]}, {"id": child["id"]}}
		if err = s.EnrichWorkHierarchy(readCtx, items, actor, true, time.Now()); err != nil {
			t.Fatal(err)
		}
		wantCount, wantParent := 0, older["ref"]
		if actor == "stranger" {
			wantParent = nil
		}
		if items[0]["rollup"].(*WorkRollup).Total != wantCount || items[1]["parent_ref"] != wantParent {
			t.Fatalf("%s: %v", actor, items)
		}
		if actor == "stranger" {
			raw, _ := json.Marshal(items)
			if strings.Contains(string(raw), workString(older["ref"])) || strings.Contains(string(raw), "Private older parent") {
				t.Fatalf("hidden parent leaked: %s", raw)
			}
		}
	}
}

func TestWatchCompletionChronologicalOrdering(t *testing.T) {
	for _, stamps := range []struct{ name, older, newer string }{
		{"fraction", "2026-10-24T12:00:00.1Z", "2026-10-24T12:00:00.11Z"},
		{"whole-second", "2026-10-24T12:00:00Z", "2026-10-24T12:00:00.1Z"},
		{"nanosecond", "2026-10-24T12:00:00.1Z", "2026-10-24T12:00:00.100000001Z"},
		{"offset", "2026-10-24T13:00:00.1+01:00", "2026-10-24T12:00:00.11Z"},
	} {
		for _, hidden := range []bool{false, true} {
			name := stamps.name + "/readable"
			if hidden {
				name = stamps.name + "/unreadable-newer"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				ws, err := initializeTestWorkspace(ctx, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer ws.Close()
				s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
				root, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Initiative"})
				if err != nil {
					t.Fatal(err)
				}
				child, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Shipped", "under": root["ref"]})
				if err != nil {
					t.Fatal(err)
				}
				private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
					t.Fatal(err)
				}
				if _, err = ws.DB().Exec(`UPDATE cards SET column_key='done' WHERE id=?`, child["id"]); err != nil {
					t.Fatal(err)
				}
				for i, stamp := range []string{stamps.older, stamps.newer} {
					refs := []string{workString(child["ref"])}
					if i == 1 && hidden {
						refs = append(refs, "board:"+workString(private["id"]))
					}
					event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "card_moved", "thread_id": child["thread_id"], "refs": refs, "payload": map[string]any{"card_id": child["id"], "column_key": "done", "from_column_key": "ready"}})
					if err != nil {
						t.Fatal(err)
					}
					// AppendEvent owns the clock. Seed historical mixed-precision
					// timestamps after canonical event/authorization creation.
					if _, err = ws.DB().Exec(`UPDATE events SET ts=?,payload_json=json_set(payload_json,'$.ts',?) WHERE id=?`, stamp, stamp, event["id"]); err != nil {
						t.Fatal(err)
					}
				}
				reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "viewer"})
				if err = s.EnrichWorkHierarchy(reader, []map[string]any{root}, "viewer", true, time.Now()); err != nil {
					t.Fatal(err)
				}
				children := root["rollup"].(*WorkRollup).Children
				if len(children) != 1 {
					t.Fatalf("children=%+v", children)
				}
				at := children[0].DoneAt
				if hidden {
					if at != nil {
						t.Fatalf("unreadable newer event fell back to %q", *at)
					}
				} else if at == nil || *at != stamps.newer {
					t.Fatalf("completion=%+v, want %s", children[0], stamps.newer)
				}
			})
		}
	}
}
