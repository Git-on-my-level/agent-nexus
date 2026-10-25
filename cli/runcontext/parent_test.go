package runcontext

import (
	"context"
	"strings"
	"testing"
	"time"
)

func fakeResolver(t *testing.T, script string) *Resolver {
	t.Helper()
	return &Resolver{Getenv: func(key string) string {
		switch key {
		case "AGENTCTL_EXECUTION_ID":
			return "exec-test"
		case "AGENTCTL_BIN":
			return "/test/agentctl"
		}
		return ""
	}, Status: func(ctx context.Context, path, id string) ([]byte, error) {
		if path != "/test/agentctl" || id != "exec-test" {
			t.Fatalf("unexpected status lookup %s %s", path, id)
		}
		if strings.Contains(script, "sleep") {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		start, end := strings.Index(script, "{"), strings.LastIndex(script, "}")
		if start < 0 {
			return []byte("not json"), nil
		}
		return []byte(script[start : end+1]), nil
	}}
}
func TestRunParentDocumentedStatusArgv(t *testing.T) {
	raw, err := readStatus(context.Background(), "/bin/echo", "exec-test")
	if err != nil || strings.TrimSpace(string(raw)) != "status exec-test --output json" {
		t.Fatalf("raw=%q err=%v", raw, err)
	}
}
func TestRunParentLookup(t *testing.T) {
	cases := []struct{ name, script, parent, code string }{
		{"label", `[ "$*" = 'status exec-test --output json' ] || exit 1
printf '%s' '{"ok":true,"result":{"id":"exec-test","labels":["other","anx.card.launch"]}}'`, "card:launch", ""},
		{"no label", `printf '%s' '{"ok":true,"result":{"id":"exec-test","labels":["other"]}}'`, "", ""},
		{"multiple", `printf '%s' '{"ok":true,"result":{"id":"exec-test","labels":["anx.card.one","anx.card.two"]}}'`, "", "run_parent_ambiguous"},
		{"invalid slug", `printf '%s' '{"ok":true,"result":{"id":"exec-test","labels":["anx.card.../bad"]}}'`, "", ""},
		{"wrong execution", `printf '%s' '{"ok":true,"result":{"id":"exec-other","labels":["anx.card.launch"]}}'`, "", ""},
		{"error envelope", `printf '%s' '{"ok":false,"result":{"id":"exec-test","labels":["anx.card.launch"]}}'`, "", ""},
		{"bad json", `printf 'not json'`, "", ""},
		{"timeout", `/bin/sleep 5`, "", "run_parent_unavailable"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeResolver(t, tt.script)
			started := time.Now()
			h := r.Lookup(context.Background())
			if h.Parent != tt.parent {
				t.Fatalf("hint=%+v", h)
			}
			if tt.code != "" && (len(h.Warnings) != 1 || h.Warnings[0].Code != tt.code) {
				t.Fatalf("hint=%+v", h)
			}
			if tt.code == "" && len(h.Warnings) > 0 {
				t.Fatal(h)
			}
			if time.Since(started) > time.Second {
				t.Fatal("lookup exceeded latency bound")
			}
			// Changing the binary after lookup must not change the cached result.
			r.Getenv = func(string) string { t.Fatal("lookup was not cached"); return "" }
			if again := r.Lookup(context.Background()); again.Parent != h.Parent {
				t.Fatal(again)
			}
		})
	}
}
func TestRunParentNoAgentctl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := &Resolver{Getenv: func(k string) string {
		if k == "AGENTCTL_EXECUTION_ID" {
			return "exec-test"
		}
		return ""
	}}
	if h := r.Lookup(context.Background()); h.Parent != "" || len(h.Warnings) > 0 {
		t.Fatal(h)
	}
}
func TestRunParentApply(t *testing.T) {
	for _, tt := range []struct {
		name          string
		body          map[string]any
		top, readable bool
		want, code    string
	}{
		{"present", map[string]any{}, false, true, "card:launch", ""},
		{"explicit parent", map[string]any{"under": "card:chosen"}, false, false, "card:chosen", ""},
		{"explicit null", map[string]any{"under": nil}, false, false, "", ""},
		{"top level", map[string]any{}, true, false, "", ""},
		{"other workspace", map[string]any{}, false, false, "", "run_parent_unreadable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fakeResolver(t, `printf '%s' '{"ok":true,"result":{"id":"exec-test","labels":["anx.card.launch"]}}'`)
			calls := 0
			hint := r.Apply(WithTopLevel(context.Background(), tt.top), tt.body, func(ctx context.Context, ref string) bool {
				calls++
				if ref != "card:launch" {
					t.Fatal(ref)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded read")
				}
				return tt.readable
			})
			got, _ := tt.body["under"].(string)
			if got != tt.want {
				t.Fatal(tt.body)
			}
			if (tt.top || strings.HasPrefix(tt.name, "explicit")) && calls != 0 {
				t.Fatal("override triggered lookup")
			}
			if tt.code != "" && (len(hint.Warnings) != 1 || hint.Warnings[0].Code != tt.code) {
				t.Fatal(hint)
			}
		})
	}
}

func TestRunParentBatchAndNestedOverrides(t *testing.T) {
	r := fakeResolver(t, `{"ok":true,"result":{"id":"exec-test","labels":["anx.card.parent"]}}`)
	items := []any{map[string]any{"title": "default"}, map[string]any{"card": map[string]any{"under": "card:chosen"}}, map[string]any{"under": nil}, map[string]any{"card": map[string]any{"title": "also default"}}}
	reads := 0
	body := map[string]any{"items": items}
	hint := r.ApplyBatch(context.Background(), body, func(context.Context, string) bool { reads++; return true })
	items = body["items"].([]any)
	if reads != 1 || len(hint.Defaulted) != 2 || hint.Defaulted[0] != 0 || hint.Defaulted[1] != 3 {
		t.Fatal(hint, reads)
	}
	for _, i := range []int{0, 3} {
		if items[i].(map[string]any)["under"] != "card:parent" {
			t.Fatal(items)
		}
	}
	if _, overridden := items[1].(map[string]any)["under"]; overridden {
		t.Fatal("nested explicit parent overwritten")
	}
}
