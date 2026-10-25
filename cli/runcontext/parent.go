// Package runcontext implements the optional, local agentctl card-parent hint.
// It never reads agentctl's private state or grants workspace access.
package runcontext

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const LookupTimeout = 250 * time.Millisecond
const outputLimit = 64 << 10

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Warning struct{ Code, Message string }
type Hint struct {
	Defaulted     []int
	Parent, Label string
	Warnings      []Warning
}

// Resolver caches the execution lookup (including failures) for the owning
// CLI/MCP process. Workspace readability is checked separately on each create.
type Resolver struct {
	once   sync.Once
	hint   Hint
	Getenv func(string) string
	Status func(context.Context, string, string) ([]byte, error)
}

func (r *Resolver) Lookup(ctx context.Context) Hint {
	r.once.Do(func() {
		getenv := r.Getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		id := strings.TrimSpace(getenv("AGENTCTL_EXECUTION_ID"))
		if id == "" {
			return
		}
		path := strings.TrimSpace(getenv("AGENTCTL_BIN"))
		if path == "" {
			var err error
			path, err = exec.LookPath("agentctl")
			if err != nil {
				return
			}
		}
		lookupCtx, cancel := context.WithTimeout(ctx, LookupTimeout)
		defer cancel()
		status := r.Status
		if status == nil {
			status = readStatus
		}
		raw, err := status(lookupCtx, path, id)
		if err != nil {
			if lookupCtx.Err() != nil {
				r.hint.Warnings = []Warning{{"run_parent_unavailable", "Run label lookup timed out; no parent default applied. Pass --under card:<slug> explicitly."}}
			}
			return
		}

		var envelope struct {
			OK     bool `json:"ok"`
			Result struct {
				ID     string   `json:"id"`
				Labels []string `json:"labels"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &envelope) != nil || !envelope.OK || envelope.Result.ID != id {
			return
		}
		var labels []string
		seen := map[string]bool{}
		for _, label := range envelope.Result.Labels {
			if strings.HasPrefix(label, "anx.card.") && !seen[label] {
				labels = append(labels, label)
				seen[label] = true
			}
		}
		if len(labels) > 1 {
			r.hint.Warnings = []Warning{{"run_parent_ambiguous", "Several anx.card.* run labels; no parent default applied. Pass --under card:<slug> explicitly."}}
		} else if len(labels) == 1 {
			slug := strings.TrimPrefix(labels[0], "anx.card.")
			if slugPattern.MatchString(slug) {
				r.hint.Parent, r.hint.Label = "card:"+slug, labels[0]
			}
		}
	})
	return r.hint
}

// ApplyBatch resolves the run parent once for at most 100 card inputs.
func (r *Resolver) ApplyBatch(ctx context.Context, body map[string]any, read func(context.Context, string) bool) Hint {
	if original, ok := body["items"].([]any); ok && len(original) <= 100 {
		items := make([]any, len(original))
		for i, raw := range original {
			if item, ok := raw.(map[string]any); ok {
				copyItem := make(map[string]any, len(item))
				for k, v := range item {
					copyItem[k] = v
				}
				items[i] = copyItem
			} else {
				items[i] = raw
			}
		}
		body["items"] = items
		var indexes []int
		for i, raw := range items {
			if item, ok := raw.(map[string]any); ok && !hasParent(item) {
				indexes = append(indexes, i)
			}
		}
		if len(indexes) == 0 || TopLevel(ctx) {
			return Hint{}
		}
		hint := r.Apply(ctx, items[indexes[0]].(map[string]any), read)
		if hint.Parent == "" {
			return hint
		}
		for _, i := range indexes[1:] {
			items[i].(map[string]any)["under"] = hint.Parent
		}
		hint.Defaulted = indexes
		return hint
	}
	return Hint{}
}

type limitedOutput struct{ data []byte }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > outputLimit {
		return 0, fmt.Errorf("agentctl status output too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

type overrideKey struct{}

func WithTopLevel(ctx context.Context, value bool) context.Context {
	return context.WithValue(ctx, overrideKey{}, value)
}
func TopLevel(ctx context.Context) bool { value, _ := ctx.Value(overrideKey{}).(bool); return value }

// Apply honors authored JSON under (including null) and explicit opt-out before
// even launching agentctl. read must use the currently selected workspace and
// principal; no other workspace is searched.
func (r *Resolver) Apply(ctx context.Context, body map[string]any, read func(context.Context, string) bool) Hint {
	if body == nil {
		return Hint{}
	}
	if hasParent(body) || TopLevel(ctx) {
		return Hint{}
	}
	hint := r.Lookup(ctx)
	if hint.Parent == "" {
		return hint
	}
	readCtx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	if !read(readCtx, hint.Parent) {
		return Hint{Warnings: []Warning{{"run_parent_unreadable", "The run label's card is not readable in the selected workspace; no parent default applied. Pass --under card:<slug> explicitly."}}}
	}
	body["under"] = hint.Parent
	return hint
}

func (h Hint) Message() string {
	return fmt.Sprintf("Created under %s (from run label %s); pass --top-level to opt out", h.Parent, h.Label)
}

// UndoArgv selects the exact child ref through the existing plan read/CAS
// writer, without depending on core's private storage ID or step-ID algorithm.
func (h Hint) UndoArgv(ref string) []string {
	if h.Parent == "" || !strings.HasPrefix(ref, "card:") {
		return nil
	}
	return []string{"anx", "plan", "step", "rm", h.Parent, "--ref", ref}
}

func readStatus(ctx context.Context, path, id string) ([]byte, error) {
	// --output json is documented by agentctl help; status does not accept --json.
	cmd := exec.CommandContext(ctx, path, "status", id, "--output", "json")
	// agentctl needs its own context only; never forward ANX credentials.
	cmd.Env = []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PATH" || key == "HOME" || strings.HasPrefix(key, "AGENTCTL_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.WaitDelay = 25 * time.Millisecond
	var out limitedOutput
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	err := cmd.Run()
	return out.data, err
}

// BudgetWarning uses one bounded work page; hidden/unreturned cards never enter
// the count. It intentionally reports a lower bound rather than walking pages.
func BudgetWarning(body map[string]any) string {
	rows, _ := body["work"].([]any)
	count := 0
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if row["parent_ref"] != nil && row["parent_ref"] != "" || row["cadence"] != nil || row["goal"] != nil || row["loop"] != nil {
			continue
		}
		phase, _ := row["phase"].(string)
		switch phase {
		case "done", "resolved", "closed", "cancelled", "canceled", "archived", "trashed":
			continue
		}
		count++
	}
	if count > 15 {
		return fmt.Sprintf("%d open initiatives (initiative budget 15); prefer grouping under a likely home", count)
	}
	return ""
}

// ReadableParent requires a visible, live card. Closed/archived parents cannot
// accept new children; skipping them keeps the optional default non-blocking.
func ReadableParent(work map[string]any, ref string) bool {
	if work["ref"] != ref {
		return false
	}
	for _, key := range []string{"archived_at", "trashed_at"} {
		if value, ok := work[key].(string); ok && value != "" {
			return false
		}
	}
	phase, _ := work["phase"].(string)
	switch phase {
	case "done", "resolved", "closed", "cancelled", "canceled", "archived", "trashed":
		return false
	}
	return true
}

func hasParent(body map[string]any) bool {
	if _, ok := body["under"]; ok {
		return true
	}
	card, _ := body["card"].(map[string]any)
	_, ok := card["under"]
	return ok
}
func IsCreateCommand(id string) bool {
	switch id {
	case "work.create", "cards.create", "boards.cards.add", "boards.cards.batch_add":
		return true
	}
	return false
}
