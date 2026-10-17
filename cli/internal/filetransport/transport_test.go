//go:build darwin || linux

package filetransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"agent-nexus-cli/internal/integrations"
)

type fixture struct {
	t                         *testing.T
	dir, project              string
	b                         Binding
	now                       time.Time
	calls                     []integrations.Delivery
	prepare                   int
	sendErr, authErr, readErr error
	c                         Callbacks
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	project, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return e
		})
	})
	f := &fixture{t: t, dir: filepath.Join(t.TempDir(), "state"), project: project, now: time.Now().UTC()}
	f.b = Binding{ID: UUID(), Directory: project, Workspace: "workspace", BaseURL: "https://core.example.test", Host: "host", Actor: "actor", Agent: "worker", Cards: []Card{{ID: "card-id", Ref: "card:work"}}}
	f.c = Callbacks{Validate: func(context.Context, Binding) error { return f.authErr }, Prepare: func(_ context.Context, b Binding, in Entry, at time.Time) (integrations.Delivery, []string, error) {
		f.prepare++
		key := "anx-file-v1-" + b.ID + "-" + in.ID
		return integrations.Delivery{Key: key, PrincipalID: b.Actor, HostID: b.Host, BaseURL: b.BaseURL, Agent: b.Agent, CommandID: "events.create", Body: map[string]any{"request_key": key, "actor_id": b.Actor, "text": in.Text, "at": at.Format(time.RFC3339Nano)}}, nil, nil
	}, Send: func(_ context.Context, _ Binding, d integrations.Delivery) (string, error) {
		f.calls = append(f.calls, d)
		return "event-one", f.sendErr
	}, Refresh: func(_ context.Context, _ Binding, c Card) (Snapshot, error) {
		return Snapshot{Card: c, Version: "version-one", Body: "Visible note"}, f.readErr
	}, Classify: func(e error) string {
		if errors.Is(e, integrations.ErrIdentityChanged) {
			return "identity"
		}
		if e != nil && e.Error() == "denied" {
			return "denied"
		}
		return "retry"
	}, Code: func(e error) string { return e.Error() }}
	if _, e = Enable(context.Background(), f.dir, f.b, f.now); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *fixture) write(in Entry) []byte {
	f.t.Helper()
	raw := integrations.JSON(in)
	if e := os.WriteFile(filepath.Join(f.project, ".anx", "outbox", in.ID+".json"), raw, 0600); e != nil {
		f.t.Fatal(e)
	}
	return raw
}
func (f *fixture) entry() Entry {
	return Entry{Version: 1, ID: UUID(), Card: "card:work", Text: "Progress from isolated writer", Evidence: []string{}, Next: &Next{At: "2d", What: "Review"}}
}
func (f *fixture) tick() State {
	f.t.Helper()
	s, e := Tick(context.Background(), f.dir, f.now, f.c)
	if e != nil {
		f.t.Fatal(e)
	}
	return s
}
func TestFrozenReplayDuplicateAndIntakeReceipt(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	raw := f.write(in)
	f.sendErr = fmt.Errorf("unknown outcome")
	s, e := Tick(context.Background(), f.dir, f.now, f.c)
	if e == nil || len(s.Pending) != 1 {
		t.Fatal(s, e)
	}
	r, e := readRecord(f.dir, in.ID)
	if e != nil || r.FirstAttempt.IsZero() || r.RetryAt.IsZero() || r.Delivery == nil {
		t.Fatal(r, e)
	}
	// Edit the source object in memory: the retry must use the durable original.
	f.now = f.now.Add(time.Minute)
	f.sendErr = nil
	f.tick()
	f.tick()
	if len(f.calls) != 2 || f.prepare != 1 || string(integrations.JSON(f.calls[0])) != string(integrations.JSON(f.calls[1])) {
		t.Fatal(f.calls, f.prepare)
	}
	got, e := os.ReadFile(filepath.Join(f.project, ".anx/outbox/.done", in.ID+".json"))
	if e != nil || string(got) != string(raw) {
		t.Fatal(string(got), e)
	}
	receipt, e := os.ReadFile(filepath.Join(f.project, ".anx/outbox/.done", in.ID+".receipt.json"))
	if e != nil || !strings.Contains(string(receipt), "event-one") {
		t.Fatal(string(receipt), e)
	}
	f.write(in)
	f.tick()
	if len(f.calls) != 2 {
		t.Fatal("producer retry duplicated note")
	}
}
func TestEditedUUIDFreezesOriginalAndBlocksOvertaking(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	raw := f.write(in)
	f.sendErr = fmt.Errorf("timeout")
	_, _ = Tick(context.Background(), f.dir, f.now, f.c)
	in.Text = "Changed bytes"
	f.write(in)
	later := f.entry()
	f.write(later)
	f.now = f.now.Add(time.Hour)
	f.sendErr = nil
	f.tick()
	if len(f.calls) != 1 {
		t.Fatal("edited UUID was replayed or later promise overtook it", f.calls)
	}
	r, e := readRecord(f.dir, in.ID)
	if e != nil || r.State != "conflict" || string(r.Raw) != string(raw) {
		t.Fatal(r, e)
	}
	if _, e = Inspect(f.dir, f.now); e == nil {
		t.Fatal("doctor missed conflict")
	}
}
func TestReplayCutoffAndAllowlist(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	f.write(in)
	f.sendErr = fmt.Errorf("timeout")
	_, _ = Tick(context.Background(), f.dir, f.now, f.c)
	f.now = f.now.Add(ReplayCutoff)
	f.sendErr = nil
	f.tick()
	if len(f.calls) != 1 {
		t.Fatal("expired key replayed")
	}
	r, _ := readRecord(f.dir, in.ID)
	if r.State != "uncertain" {
		t.Fatal(r)
	}
	g := newFixture(t)
	bad := g.entry()
	bad.Card = "card:outside"
	g.write(bad)
	s := g.tick()
	if len(g.calls) != 0 || s.Failed != 1 {
		t.Fatal(s)
	}
	r, _ = readRecord(g.dir, bad.ID)
	if r.Reason != "card_not_allowlisted" {
		t.Fatal(r)
	}
}
func TestRecoveryAfterClaimAndOrphanRecord(t *testing.T) {
	for _, orphan := range []bool{false, true} {
		t.Run(fmt.Sprint(orphan), func(t *testing.T) {
			f := newFixture(t)
			in := f.entry()
			raw := f.write(in)
			path := filepath.Join(f.project, ".anx")
			claim := in.ID + "." + UUID() + ".json"
			if e := os.Rename(filepath.Join(path, "outbox", in.ID+".json"), filepath.Join(path, "intake", claim)); e != nil {
				t.Fatal(e)
			}
			if orphan {
				r := Record{ID: in.ID, Hash: Digest(raw), Raw: raw, Input: in, Sequence: 1, Received: f.now, State: "pending"}
				if e := saveRecord(f.dir, r); e != nil {
					t.Fatal(e)
				}
			}
			s := f.tick()
			f.tick()
			if len(f.calls) != 1 || s.Records != 1 || len(s.Pending) != 0 {
				t.Fatal(s, f.calls)
			}
		})
	}
}
func TestRevocationOfflineStalenessAndDisable(t *testing.T) {
	for _, stage := range []string{"validate", "refresh", "send", "disabled"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t)
			f.tick()
			f.tick()
			path := filepath.Join(f.project, ".anx")
			if b, e := os.ReadFile(filepath.Join(path, "cards/card-id.md")); e != nil || !strings.Contains(string(b), "Visible note") {
				t.Fatal(string(b), e)
			}
			in := f.entry()
			f.write(in)
			switch stage {
			case "validate":
				f.authErr = integrations.ErrIdentityChanged
			case "refresh":
				f.readErr = fmt.Errorf("denied")
			case "send":
				f.sendErr = integrations.ErrIdentityChanged
			case "disabled":
				if _, e := Disable(context.Background(), f.dir, f.now); e != nil {
					t.Fatal(e)
				}
			}
			if stage != "disabled" {
				_, _ = Tick(context.Background(), f.dir, f.now, f.c)
			}
			if _, e := os.Stat(filepath.Join(path, "cards/card-id.md")); !os.IsNotExist(e) {
				t.Fatal("revoked card still reachable", e)
			}
			entries, e := os.ReadDir(filepath.Join(path, "mirror/generations"))
			if e != nil {
				t.Fatal(e)
			}
			for _, g := range entries {
				b, _ := os.ReadFile(filepath.Join(path, "mirror/generations", g.Name(), "cards/card-id.md"))
				if len(b) > 0 {
					t.Fatal("old revoked generation retained")
				}
			}
			if _, e = os.Stat(snapshotPath(f.dir, "card-id")); !os.IsNotExist(e) {
				t.Fatal("private cache retained", e)
			}
			if stage == "send" || stage == "validate" {
				s, _ := Load(f.dir)
				if len(s.Pending) == 0 && stage == "send" {
					t.Fatal("lost original")
				}
			}
		})
	}
	f := newFixture(t)
	f.tick()
	f.now = f.now.Add(6 * time.Minute)
	f.authErr = fmt.Errorf("offline")
	_, _ = Tick(context.Background(), f.dir, f.now, f.c)
	raw, e := os.ReadFile(filepath.Join(f.project, ".anx/STATUS.json"))
	var status Status
	_ = json.Unmarshal(raw, &status)
	if e != nil || !status.Stale || len(f.calls) != 0 {
		t.Fatal(string(raw), e)
	}
	if _, e = os.Stat(filepath.Join(f.project, ".anx/cards/card-id.md")); e != nil {
		t.Fatal("offline should retain timestamped cache", e)
	}
}
func TestCapacityBackpressureNeverEvicts(t *testing.T) {
	f := newFixture(t)
	s, _ := Load(f.dir)
	s.Records = MaxRecords
	if e := Save(f.dir, s); e != nil {
		t.Fatal(e)
	}
	in := f.entry()
	raw := f.write(in)
	s = f.tick()
	got, e := os.ReadFile(filepath.Join(f.project, ".anx/outbox", in.ID+".json"))
	if e != nil || string(raw) != string(got) || len(f.calls) != 0 || s.LastCode != "capacity" {
		t.Fatal(s, e)
	}
}
func TestUnsafeInputsAndPathReplacement(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "oversize", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			outside := filepath.Join(t.TempDir(), "sentinel")
			if e := os.WriteFile(outside, []byte("unchanged"), 0600); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(f.project, ".anx/outbox", UUID()+".json")
			var e error
			switch kind {
			case "symlink":
				e = os.Symlink(outside, path)
			case "hardlink":
				e = os.Link(outside, path)
			case "fifo":
				e = syscall.Mkfifo(path, 0600)
			case "oversize":
				e = os.WriteFile(path, []byte(strings.Repeat("x", MaxEntry+1)), 0600)
			case "directory":
				e = os.Mkdir(path, 0700)
			}
			if e != nil {
				t.Fatal(e)
			}
			f.tick()
			if len(f.calls) != 0 {
				t.Fatal("unsafe entry sent")
			}
			intake, e := os.ReadDir(filepath.Join(f.project, ".anx/intake"))
			if e != nil || len(intake) != 0 {
				t.Fatal("unsafe claim not terminal", intake, e)
			}
			failed, e := os.ReadDir(filepath.Join(f.project, ".anx/outbox/.failed"))
			if e != nil || len(failed) != 2 {
				t.Fatal("missing quarantined object and receipt", failed, e)
			}
			var object string
			for _, item := range failed {
				p := filepath.Join(f.project, ".anx/outbox/.failed", item.Name())
				if strings.HasSuffix(item.Name(), ".receipt.json") {
					receipt, err := os.ReadFile(p)
					if err != nil || !strings.Contains(string(receipt), "unsafe_or_oversize_entry") {
						t.Fatal(string(receipt), err)
					}
				} else {
					object = p
				}
			}
			before, err := os.Lstat(object)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if target, err := os.Readlink(object); err != nil || target != outside {
					t.Fatal(target, err)
				}
			}
			if kind == "oversize" && before.Size() != MaxEntry+1 {
				t.Fatal("original truncated", before)
			}
			f.write(f.entry())
			f.tick()
			f.tick()
			if len(f.calls) != 1 {
				t.Fatal("terminal claim blocked later valid entry", f.calls)
			}
			after, err := os.Lstat(object)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("quarantined object replaced", err)
			}

			b, e := os.ReadFile(outside)
			if e != nil || string(b) != "unchanged" {
				t.Fatal("sentinel changed", e)
			}
		})
	}
	f := newFixture(t)
	f.tick()
	outside := t.TempDir()
	path := filepath.Join(f.project, ".anx/intake")
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, path); e != nil {
		t.Fatal(e)
	}
	f.write(f.entry())
	if _, e := Tick(context.Background(), f.dir, f.now, f.c); e == nil {
		t.Fatal("symlink parent accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("escaped scope")
	}
}
func TestDecodeRejectsAuthorityAndAmbiguity(t *testing.T) {
	id := UUID()
	valid := fmt.Sprintf(`{"schema_version":1,"id":%q,"card_ref":"card:work","text":"x","evidence_refs":[]}`, id)
	if _, e := Decode([]byte(valid)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{strings.Replace(valid, `"text":"x"`, `"text":"x","text":"y"`, 1), strings.Replace(valid, `"text":"x"`, `"text":"x","actor_id":"other"`, 1), strings.Replace(valid, `"text":"x"`, `"text":"x","status":"done"`, 1), valid + ` {}`, strings.Replace(valid, id, "../../escape", 1)} {
		if _, e := Decode([]byte(raw)); e == nil {
			t.Fatal("accepted ambiguous or authoritative entry", raw)
		}
	}
}
func TestFailedPublicationKeepsCurrentGeneration(t *testing.T) {
	f := newFixture(t)
	f.tick()
	path := filepath.Join(f.project, ".anx/mirror/current")
	before, e := os.Readlink(path)
	if e != nil {
		t.Fatal(e)
	}
	fs, e := openBinding(f.b)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	fs.writeFailure = func(path string) error {
		if strings.HasSuffix(path, "STATUS.json") {
			return syscall.ENOSPC
		}
		return nil
	}
	s, _ := Load(f.dir)
	e = publish(fs, s, []Snapshot{{Card: f.b.Cards[0], AsOf: f.now, Body: "not committed"}}, f.now, false)
	if !errors.Is(e, syscall.ENOSPC) {
		t.Fatal(e)
	}
	after, _ := os.Readlink(path)
	if before != after {
		t.Fatal("incomplete generation became visible")
	}
	b, e := os.ReadFile(filepath.Join(f.project, ".anx/cards/card-id.md"))
	if e != nil || strings.Contains(string(b), "not committed") {
		t.Fatal(string(b), e)
	}
}
func TestPendingBlocksBindingChangeAndDisablePreservesOriginal(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	raw := f.write(in)
	f.sendErr = fmt.Errorf("timeout")
	_, _ = Tick(context.Background(), f.dir, f.now, f.c)
	if _, e := Disable(context.Background(), f.dir, f.now); e != nil {
		t.Fatal(e)
	}
	r, e := readRecord(f.dir, in.ID)
	if e != nil || string(r.Raw) != string(raw) {
		t.Fatal(r, e)
	}
	changed := f.b
	changed.Cards = []Card{{ID: "other", Ref: "card:other"}}
	if _, e = Enable(context.Background(), f.dir, changed, f.now); e == nil {
		t.Fatal("rerouted pending input")
	}
	if _, e = Enable(context.Background(), f.dir, f.b, f.now); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Minute)
	f.sendErr = nil
	f.tick()
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
}

func TestPersistenceFailureRetainsClaimBeforeAnySend(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	raw := f.write(in)
	// An unwriteable journal namespace models persistence failure before admission.
	entries := filepath.Join(f.dir, "entries")
	if e := os.WriteFile(entries, []byte("not a directory"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Tick(context.Background(), f.dir, f.now, f.c); e == nil {
		t.Fatal("ignored journal failure")
	}
	if len(f.calls) != 0 {
		t.Fatal("sent before durable admission")
	}
	claims, e := os.ReadDir(filepath.Join(f.project, ".anx/intake"))
	if e != nil || len(claims) != 1 {
		t.Fatal(claims, e)
	}
	got, e := os.ReadFile(filepath.Join(f.project, ".anx/intake", claims[0].Name()))
	if e != nil || string(got) != string(raw) {
		t.Fatal("lost original", e)
	}
	if e = os.Remove(entries); e != nil {
		t.Fatal(e)
	}
	f.tick()
	if len(f.calls) != 1 {
		t.Fatal("did not recover durable claim")
	}
}
func TestPrepareFailureIsInspectableAndExpires(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	f.write(in)
	f.c.Prepare = func(context.Context, Binding, Entry, time.Time) (integrations.Delivery, []string, error) {
		return integrations.Delivery{}, nil, fmt.Errorf("prepare unavailable")
	}
	_, e := Tick(context.Background(), f.dir, f.now, f.c)
	if e == nil {
		t.Fatal("missing error")
	}
	r, e := readRecord(f.dir, in.ID)
	if e != nil || r.FirstAttempt.IsZero() || !r.RetryAt.After(f.now) || r.Reason == "" || len(f.calls) != 0 {
		t.Fatal(r, e)
	}
	f.now = f.now.Add(ReplayCutoff)
	f.tick()
	r, _ = readRecord(f.dir, in.ID)
	if r.State != "uncertain" {
		t.Fatal("prepare retried indefinitely", r)
	}
}
func TestGenerationConsistencyAcrossAtomicSwap(t *testing.T) {
	f := newFixture(t)
	fs, e := openBinding(f.b)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	// Publish twice: both current and one prior complete generation stay coherent.
	if e = purgeMirror(fs); e != nil {
		t.Fatal(e)
	}
	f.tick()
	prior, e := os.Readlink(filepath.Join(f.project, ".anx/mirror/current"))
	if e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Minute)
	f.tick()
	after, e := os.Readlink(filepath.Join(f.project, ".anx/mirror/current"))
	if e != nil || after == prior {
		t.Fatal(after, e)
	}
	for _, target := range []string{prior, after} {
		raw, e := os.ReadFile(filepath.Join(f.project, ".anx/mirror", target, "STATUS.json"))
		var status Status
		_ = json.Unmarshal(raw, &status)
		if e != nil || filepath.Base(target) != status.Generation {
			t.Fatal(string(raw), e)
		}
		card, e := os.ReadFile(filepath.Join(f.project, ".anx/mirror", target, "cards/card-id.md"))
		if e != nil || !strings.Contains(string(card), "generation: "+status.Generation) {
			t.Fatal(string(card), e)
		}
	}
}
func TestStableRootDescriptorCannotEscapeAfterRename(t *testing.T) {
	f := newFixture(t)
	fs, e := openBinding(f.b)
	if e != nil {
		t.Fatal(e)
	}
	defer fs.Close()
	old := filepath.Join(f.project, ".anx")
	moved := filepath.Join(f.project, ".anx-moved")
	if e = os.Rename(old, moved); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	if e = os.Symlink(outside, old); e != nil {
		t.Fatal(e)
	}
	if e = fs.write("intake/safe", []byte("old descriptor"), 0600); e != nil {
		t.Fatal(e)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("descriptor operation followed replacement")
	}
	if _, e = openBinding(f.b); e == nil {
		t.Fatal("reopened through replacement symlink")
	}
}

func TestInitialEnableRecoversPersistedUUIDWithoutMarker(t *testing.T) {
	project, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return e
		})
	})
	dir := filepath.Join(t.TempDir(), "state")
	b := Binding{ID: UUID(), Directory: project, Workspace: "workspace", BaseURL: "https://core.example.test", Host: "host", Actor: "actor", Agent: "worker", Cards: []Card{{ID: "card-id", Ref: "card:work"}}}
	if e = Save(dir, State{Version: 1, Binding: b, Pending: []string{}}); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(project, ".anx"), 0700); e != nil {
		t.Fatal(e)
	}
	s, e := Enable(context.Background(), dir, b, time.Now())
	if e != nil || s.Binding.ID != b.ID || !s.Enabled {
		t.Fatal(s, e)
	}
}

func TestConcurrentEnableCannotReplaceDirectoryWriter(t *testing.T) {
	for i := 0; i < 8; i++ {
		project, e := filepath.EvalSymlinks(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
				if e == nil && d.IsDir() {
					return os.Chmod(path, 0700)
				}
				return e
			})
		})
		a := Binding{ID: UUID(), Directory: project, Workspace: "workspace", BaseURL: "https://core.example.test", Host: "one", Actor: "actor-one", Agent: "one", Cards: []Card{{ID: "card", Ref: "card:work"}}}
		b := a
		b.ID = UUID()
		b.Host = "two"
		b.Actor = "actor-two"
		b.Agent = "two"
		type outcome struct {
			b Binding
			e error
		}
		out := make(chan outcome, 2)
		start := make(chan struct{})
		for _, binding := range []Binding{a, b} {
			dir := filepath.Join(t.TempDir(), "state")
			go func() {
				<-start
				_, e := Enable(context.Background(), dir, binding, time.Now())
				out <- outcome{binding, e}
			}()
		}
		close(start)
		first, second := <-out, <-out
		success := 0
		for _, r := range []outcome{first, second} {
			if r.e == nil {
				success++
				f, e := openBinding(r.b)
				if e != nil {
					t.Fatal("successful enable lost its writer binding", e)
				}
				f.Close()
			}
		}
		if success != 1 {
			t.Fatal("directory must elect exactly one writer", first.e, second.e)
		}
	}
}

func TestEnableRecoversItsOwnInterruptedMarkerPublication(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(fmt.Sprint(linked), func(t *testing.T) {
			project, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
					if e == nil && d.IsDir() {
						return os.Chmod(path, 0700)
					}
					return e
				})
			})
			dir := filepath.Join(t.TempDir(), "state")
			b := Binding{ID: UUID(), Directory: project, Workspace: "workspace", BaseURL: "https://core.example.test", Host: "host", Actor: "actor", Agent: "worker", Cards: []Card{{ID: "card", Ref: "card:work"}}}
			if e = Save(dir, State{Version: 1, Binding: b, Pending: []string{}}); e != nil {
				t.Fatal(e)
			}
			if e = os.Mkdir(filepath.Join(project, ".anx"), 0700); e != nil {
				t.Fatal(e)
			}
			fs, e := Open(filepath.Join(project, ".anx"))
			if e != nil {
				t.Fatal(e)
			}
			name := ".identity-" + b.ID
			if e = fs.write(name, integrations.JSON(map[string]any{"schema_version": 1, "directory_uuid": b.ID}), 0600); e != nil {
				fs.Close()
				t.Fatal(e)
			}
			if linked {
				if e = linkAt(fs.root, name, fs.root, "IDENTITY.json"); e != nil {
					fs.Close()
					t.Fatal(e)
				}
			}
			fs.Close()
			if _, e = Enable(context.Background(), dir, b, time.Now()); e != nil {
				t.Fatal(e)
			}
		})
	}
}
