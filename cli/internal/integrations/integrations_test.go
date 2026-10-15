package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func recipe(t *testing.T) Recipe {
	t.Helper()
	r, err := Lookup("claude-code-suggested-tasks")
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestSettingsMerge(t *testing.T) {
	r := recipe(t)
	adapter := ClaudeCode{}
	for _, raw := range []string{`{}`, `{"permissions":{"allow":["Read"]},"huge":9007199254740993,"hooks":{"Stop":[{"hooks":[{"type":"command","command":"user-stop"}]}],"PostToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"user-write"}]}]}}`} {
		installed, changed, err := adapter.Merge([]byte(raw), r, "/anx integrations run test", "", true)
		if err != nil || !changed {
			t.Fatalf("install %v %v", changed, err)
		}
		second, changed, err := adapter.Merge(installed, r, "/anx integrations run test", "", true)
		if err != nil || changed || string(second) != string(installed) {
			t.Fatalf("not idempotent %v %v", changed, err)
		}
		present, matches, err := adapter.Inspect(installed, r, "/anx integrations run test")
		if err != nil || !present || !matches {
			t.Fatal(present, matches, err)
		}
		removed, changed, err := adapter.Merge(installed, r, "/anx integrations run test", "", false)
		if err != nil || !changed {
			t.Fatal(err)
		}
		if strings.Contains(raw, "9007199254740993") && !strings.Contains(string(removed), "9007199254740993") {
			t.Fatal("number corrupted")
		}
		if strings.Contains(raw, "user-stop") && (!strings.Contains(string(removed), "user-stop") || !strings.Contains(string(removed), "user-write")) {
			t.Fatal("unrelated hook lost")
		}
		_, changed, err = adapter.Merge(removed, r, "/anx integrations run test", "", false)
		if err != nil || changed {
			t.Fatal("disable not idempotent")
		}
	}
}
func TestSettingsRejectInvalidAndEmpty(t *testing.T) {
	for _, raw := range []string{"", `{`, `null`, `[]`, `{"hooks":null}`, `{"hooks":{},"hooks":{}}`, `{"hooks":{"PostToolUse":{}}}`, `{"hooks":{"PostToolUse":[{"hooks":null}]}}`} {
		if _, _, err := (ClaudeCode{}).Merge([]byte(raw), recipe(t), "anx", "", true); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func TestSettingsSiblingsAndExactOwnership(t *testing.T) {
	r := recipe(t)
	raw := []byte(`{"hooks":{"PostToolUse":[{"matcher":"wrong","hooks":[{"type":"command","command":"old"},{"type":"command","command":"old --user"}]}]}}`)
	present, matches, err := (ClaudeCode{}).Inspect(raw, r, "old")
	if err != nil || !present || matches {
		t.Fatal("drift not detected")
	}
	got, _, err := (ClaudeCode{}).Merge(raw, r, "new", "old", true)
	if err != nil || !strings.Contains(string(got), "old --user") {
		t.Fatal("clobbered sibling", err)
	}
	if p, m, _ := (ClaudeCode{}).Inspect(got, r, "new"); !p || !m {
		t.Fatal("not repaired")
	}
}
func TestFilesAtomicBackupAndScopes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	raw, err := ReadSettings(path)
	if err != nil || string(raw) != "{}" {
		t.Fatal(err)
	}
	if err = AtomicWrite(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err = Backup(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err = AtomicWrite(path, []byte("updated")); err != nil {
		t.Fatal(err)
	}
	if err = Backup(path, []byte("updated")); err != nil {
		t.Fatal(err)
	}
	b, _ := Read(path + ".anx-backup")
	st, _ := os.Stat(path)
	if string(b) != "original" || st.Mode().Perm() != 0640 {
		t.Fatal("backup/mode not preserved")
	}
	link := filepath.Join(dir, "link")
	if err = os.Symlink(path, link); err == nil {
		if err = AtomicWrite(link, []byte("bad")); err == nil {
			t.Fatal("followed symlink")
		}
	}
	home, project := filepath.Join(dir, "home"), filepath.Join(dir, "project")
	for scope, want := range map[string]string{"user": filepath.Join(home, ".claude", "settings.json"), "project": filepath.Join(project, ".claude", "settings.json"), "local": filepath.Join(project, ".claude", "settings.local.json")} {
		got, err := (ClaudeCode{}).ConfigPath(home, project, scope)
		if err != nil || got != want {
			t.Fatal(got, err)
		}
	}
}
func payload(response string) []byte {
	return []byte(`{"hook_event_name":"PostToolUse","tool_name":"mcp__ccd_session__spawn_task","session_id":"s","tool_use_id":"u","cwd":"/project","tool_input":{"title":"Task","tldr":"Summary","prompt":"full\n` + "```" + `\nprompt"},"tool_response":` + response + `}`)
}
func TestPayloadParsing(t *testing.T) {
	r := recipe(t)
	for _, response := range []string{`"task_id: task_1234abcd"`, `[{"type":"text","text":"task_id: task_1234abcd"}]`, `{"content":[{"text":"task_1234abcd"}]}`} {
		e, err := r.Parse(payload(response))
		if err != nil || e.TaskID != "task_1234abcd" {
			t.Fatal(e, err)
		}
		body := CardBody(e, Target{Board: "tasks"})
		if !strings.Contains(body["card"].(map[string]any)["summary"].(string), "> "+strings.ReplaceAll(e.Prompt, "\n", "\n> ")) {
			t.Fatal("prompt changed")
		}
	}
	e, err := r.Parse(payload(`"no id"`))
	if err != nil || e.Key == "" || e.TaskID != "" {
		t.Fatal(e, err)
	}
	if _, err = r.Parse([]byte(strings.ReplaceAll(string(payload(`"no id"`)), `"tool_use_id":"u",`, ""))); err == nil {
		t.Fatal("accepted unidentifiable event")
	}
	if _, err = r.Parse([]byte(strings.Repeat("x", MaxPayloadBytes+1))); err == nil {
		t.Fatal("unbounded payload")
	}
}
func TestSpoolFailureReplayFrozenDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool.json")
	ctx := context.Background()
	e, _ := recipe(t).Parse(payload(`"task_1234abcd"`))
	e.Timestamp = time.Now()
	if err := Enqueue(ctx, path, e); err != nil {
		t.Fatal(err)
	}
	original := Delivery{BaseURL: "https://workspace.example", Agent: "claude-code", Body: CardBody(e, Target{Board: "tasks"})}
	prepare := func(Event) (Delivery, error) { return original, nil }
	var seen []Delivery
	fail := func(_ context.Context, d Delivery) error {
		seen = append(seen, d)
		return errors.New("unknown outcome")
	}
	if err := Drain(ctx, path, prepare, fail); err == nil {
		t.Fatal("wanted failure")
	}
	q, _ := LoadQueue(path)
	if len(q.Pending) != 1 || q.Pending[0].Delivery == nil {
		t.Fatal(q)
	}
	// A duplicate hook and a config change cannot alter an in-flight request.
	e.Prompt = "changed prompt"
	e.Timestamp = time.Now()
	if err := Enqueue(ctx, path, e); err != nil {
		t.Fatal(err)
	}
	err := Drain(ctx, path, func(Event) (Delivery, error) { t.Fatal("rerouted frozen delivery"); return Delivery{}, nil }, func(_ context.Context, d Delivery) error { seen = append(seen, d); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen[0], seen[1]) {
		t.Fatalf("retry body changed: %#v %#v", seen[0], seen[1])
	}
	q, _ = LoadQueue(path)
	if len(q.Pending) != 0 || len(q.Ledger) != 1 {
		t.Fatal(q)
	}
	if err = Enqueue(ctx, path, e); err != nil {
		t.Fatal(err)
	}
	q, _ = LoadQueue(path)
	if len(q.Pending) != 0 {
		t.Fatal("recent completed event resent")
	}
}
func TestSpoolNoBoardRetentionAndConcurrentEnqueue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spool.json")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := Event{Key: Digest("same"), Timestamp: time.Now()}
			if err := Enqueue(ctx, path, e); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := Drain(ctx, path, func(Event) (Delivery, error) { return Delivery{}, errors.New("no_board") }, func(context.Context, Delivery) error { t.Fatal("sent without board"); return nil }); err == nil {
		t.Fatal("wanted no_board")
	}
	q, _ := LoadQueue(path)
	if len(q.Pending) != 1 || q.Pending[0].Reason != "no_board" {
		t.Fatal(q)
	}
	q.Pending[0].Event.Timestamp = time.Now().Add(-MaxAge - time.Hour)
	q.prune(time.Now())
	if q.Dropped != 1 || len(q.Pending) != 0 {
		t.Fatal(q)
	}
	for i := 0; i < MaxPending+3; i++ {
		q.Pending = append(q.Pending, Pending{Event: Event{Timestamp: time.Now()}})
	}
	q.prune(time.Now())
	if q.Dropped != 4 || len(q.Pending) != MaxPending {
		t.Fatal("capacity not bounded")
	}
	b, _ := json.Marshal(q)
	if len(b) > MaxFileBytes {
		t.Fatal("queue cannot be read")
	}
}

func TestSpoolDrainBoundAndPriority(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "spool.json")
	for i := 0; i < MaxDrain+2; i++ {
		if err := Enqueue(ctx, path, Event{Key: strings.Repeat("x", i+1), Timestamp: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	var keys []string
	prepare := func(e Event) (Delivery, error) { return Delivery{Body: map[string]any{"key": e.Key}}, nil }
	fail := func(_ context.Context, d Delivery) error {
		keys = append(keys, d.Body["key"].(string))
		return errors.New("offline")
	}
	newest := strings.Repeat("x", MaxDrain+2)
	if err := Drain(ctx, path, prepare, fail, newest); err == nil {
		t.Fatal("wanted failure")
	}
	if len(keys) != MaxDrain || keys[0] != newest {
		t.Fatal("drain did not prioritize current event or exceeded bound", keys)
	}
	keys = nil
	_ = Drain(ctx, path, prepare, fail)
	if len(keys) != MaxDrain || keys[0] != strings.Repeat("x", MaxDrain) {
		t.Fatal("failed head starved replay", keys)
	}
}
func TestSpoolPersistsAgeDrops(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool.json")
	q := Queue{Pending: []Pending{{Event: Event{Key: "old", Timestamp: time.Now().Add(-MaxAge - time.Hour)}}}}
	if err := AtomicWrite(path, JSON(q)); err != nil {
		t.Fatal(err)
	}
	if err := Drain(context.Background(), path, func(Event) (Delivery, error) { t.Fatal("prepared expired event"); return Delivery{}, nil }, func(context.Context, Delivery) error { t.Fatal("sent expired event"); return nil }); err != nil {
		t.Fatal(err)
	}
	q, err := LoadQueue(path)
	if err != nil || len(q.Pending) != 0 || q.Dropped != 1 {
		t.Fatal(q, err)
	}
}

func TestSettingsPreserveUnknownGroupMetadata(t *testing.T) {
	r := recipe(t)
	raw := []byte(`{"hooks":{"PostToolUse":[{"matcher":"custom","user_metadata":{"keep":true},"hooks":[{"type":"command","command":"owned"}]}]}}`)
	next, changed, err := (ClaudeCode{}).Merge(raw, r, "owned", "", false)
	if err != nil || !changed || !strings.Contains(string(next), `"user_metadata"`) || !strings.Contains(string(next), `"keep"`) {
		t.Fatal(string(next), changed, err)
	}
}

func TestSettingsRepairInterruptedBinaryPathChange(t *testing.T) {
	r := recipe(t)
	old := HookCommand("/old/anx", "/config with ' quote", r.Name, "abc")
	next := HookCommand("/new/anx", "/config with ' quote", r.Name, "abc")
	raw, _, err := (ClaudeCode{}).Merge([]byte(`{}`), r, old, "", true)
	if err != nil {
		t.Fatal(err)
	}
	present, matches, err := (ClaudeCode{}).Inspect(raw, r, next)
	if err != nil || !present || matches {
		t.Fatal("missed stale-path ownership", present, matches, err)
	}
	repaired, _, err := (ClaudeCode{}).Merge(raw, r, next, "", true)
	if err != nil || strings.Contains(string(repaired), "/old/anx") {
		t.Fatal(string(repaired), err)
	}
	for _, candidate := range []string{old + " --user", old + " && echo unsafe", "echo " + old, HookCommand("/old/anx", "/other-config", r.Name, "abc")} {
		if ownedCommand(candidate, next) {
			t.Fatalf("loose ownership: %s", candidate)
		}
	}
}
