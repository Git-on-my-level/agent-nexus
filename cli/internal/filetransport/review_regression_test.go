//go:build darwin || linux

package filetransport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/integrations"
)

func TestOutboxFloodCannotBlockAdmittedFrozenReplay(t *testing.T) {
	f := newFixture(t)
	in := f.entry()
	f.write(in)
	f.sendErr = fmt.Errorf("unknown outcome")
	if _, e := Tick(context.Background(), f.dir, f.now, f.c); e == nil {
		t.Fatal("expected uncertain send")
	}
	frozen := string(integrations.JSON(f.calls[0]))
	for i := 0; i < MaxRecords+MaxPending+1; i++ {
		if e := os.WriteFile(filepath.Join(f.project, ".anx/outbox", fmt.Sprintf("junk-%05d", i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	f.now = f.now.Add(time.Minute)
	f.sendErr = nil
	s := f.tick()
	if len(s.Pending) != 0 || len(f.calls) != 2 || frozen != string(integrations.JSON(f.calls[1])) || s.LastCode != "capacity" {
		t.Fatal("flood blocked durable replay or hid capacity", s, f.calls)
	}
	f.tick() // Finalize the original even while admission remains over capacity.
	if _, e := os.Stat(filepath.Join(f.project, ".anx/outbox/.done", in.ID+".json")); e != nil {
		t.Fatal(e)
	}
	detail, e := Inspect(f.dir, f.now)
	if e == nil || detail["last_error_code"] != "capacity" {
		t.Fatal("doctor missed overflow", detail, e)
	}
	raw, e := os.ReadFile(filepath.Join(f.project, ".anx/STATUS.json"))
	if e != nil || !strings.Contains(string(raw), `"last_error_code": "capacity"`) {
		t.Fatal("mirror status hid capacity", string(raw), e)
	}
	entries, e := os.ReadDir(filepath.Join(f.project, ".anx/outbox"))
	if e != nil || len(entries) != MaxRecords+MaxPending+3 {
		t.Fatal("producer files changed", len(entries), e)
	}
}

func TestPendingOriginalsConsumeExactlyOneAdmissionSlot(t *testing.T) {
	f := newFixture(t)
	f.sendErr = fmt.Errorf("offline")
	for i := 0; i < 50; i++ {
		f.write(f.entry())
	}
	s, e := Tick(context.Background(), f.dir, f.now, f.c)
	if e == nil || len(s.Pending) != 50 {
		t.Fatal(s, e)
	}
	for i := 0; i < 50; i++ {
		f.write(f.entry())
	}
	// Still in backoff: all 100 slots should be occupied, not 50 counted twice.
	f.now = f.now.Add(time.Millisecond)
	s = f.tick()
	if len(s.Pending) != MaxPending || s.Records != MaxPending || s.LastCode == "capacity" {
		t.Fatal(s)
	}
}

func TestTerminalJunkDoesNotConsumePendingSlots(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < MaxPending+1; i++ {
		if e := os.WriteFile(filepath.Join(f.project, ".anx/outbox", fmt.Sprintf("000-junk-%03d.json", i)), []byte("not json"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	in := f.entry()
	f.write(in)
	f.tick()
	s := f.tick()
	if len(f.calls) != 1 || len(s.Pending) != 0 || s.Failed != MaxPending+1 {
		t.Fatal(s, f.calls)
	}
	entries, e := os.ReadDir(filepath.Join(f.project, ".anx/intake"))
	if e != nil || len(entries) != 1 {
		t.Fatal("junk remained in intake", entries, e)
	} // only the newly confirmed safe original
	f.tick()
	entries, e = os.ReadDir(filepath.Join(f.project, ".anx/intake"))
	if e != nil || len(entries) != 0 {
		t.Fatal(entries, e)
	}
}

func TestCardDenialFailsOnlyItsEntryAndEvictsOnlyItsMirror(t *testing.T) {
	for _, phase := range []string{"prepare", "send"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			f.b.Cards = append(f.b.Cards, Card{ID: "other-id", Ref: "card:other"})
			if _, e := Enable(context.Background(), f.dir, f.b, f.now); e != nil {
				t.Fatal(e)
			}
			f.tick() // Both cached, plus a retained prior generation.
			prepare, send, refresh := f.c.Prepare, f.c.Send, f.c.Refresh
			f.c.Prepare = func(ctx context.Context, b Binding, in Entry, at time.Time) (integrations.Delivery, []string, error) {
				if phase == "prepare" && in.Card == "card:work" {
					return integrations.Delivery{}, nil, fmt.Errorf("denied")
				}
				d, w, e := prepare(ctx, b, in, at)
				d.Body["card"] = in.Card
				return d, w, e
			}
			f.c.Send = func(ctx context.Context, b Binding, d integrations.Delivery) (string, error) {
				if phase == "send" && d.Body["card"] == "card:work" {
					return "", fmt.Errorf("denied")
				}
				return send(ctx, b, d)
			}
			f.c.Refresh = func(ctx context.Context, b Binding, c Card) (Snapshot, error) {
				// Other-card cache must survive even if its refresh is temporarily offline.
				if c.ID == "other-id" {
					return Snapshot{}, fmt.Errorf("offline")
				}
				return refresh(ctx, b, c)
			}
			a := f.entry()
			a.ID = "00000000-0000-4000-8000-000000000001"
			f.write(a)
			b := f.entry()
			b.ID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			b.Card = "card:other"
			f.write(b)
			s, e := Tick(context.Background(), f.dir, f.now, f.c)
			if e == nil || len(s.Pending) != 0 || s.Failed != 1 || len(f.calls) != 1 || f.calls[0].Body["card"] != "card:other" {
				t.Fatal(s, e, f.calls)
			}
			r, e := readRecord(f.dir, a.ID)
			if e != nil || r.State != "failed" || r.Reason != "denied" {
				t.Fatal(r, e)
			}
			raw, e := os.ReadFile(filepath.Join(f.project, ".anx/outbox/.failed", a.ID+".json"))
			if e != nil || string(raw) != string(integrations.JSON(a)) {
				t.Fatal(string(raw), e)
			}
			receipt, e := os.ReadFile(filepath.Join(f.project, ".anx/outbox/.failed", a.ID+".receipt.json"))
			if e != nil || !strings.Contains(string(receipt), "denied") {
				t.Fatal(string(receipt), e)
			}
			if _, e = os.Stat(snapshotPath(f.dir, "card-id")); !os.IsNotExist(e) {
				t.Fatal("denied cache retained", e)
			}
			if _, e = os.Stat(filepath.Join(f.project, ".anx/cards/other-id.md")); e != nil {
				t.Fatal("unrelated cache removed", e)
			}
			generations, e := os.ReadDir(filepath.Join(f.project, ".anx/mirror/generations"))
			if e != nil {
				t.Fatal(e)
			}
			for _, g := range generations {
				if _, e = os.Stat(filepath.Join(f.project, ".anx/mirror/generations", g.Name(), "cards/card-id.md")); !os.IsNotExist(e) {
					t.Fatal("old denied copy retained", e)
				}
			}
			later := f.entry()
			later.Card = "card:other"
			f.write(later)
			_, _ = Tick(context.Background(), f.dir, f.now, f.c)
			if len(f.calls) != 2 {
				t.Fatal("later unrelated note blocked", f.calls)
			}
		})
	}
}

func TestCardRefsMatchExactlyEvenWhenIDsCollideWithHandles(t *testing.T) {
	f := newFixture(t)
	f.b.Cards = []Card{{ID: "work", Ref: "card:other"}, {ID: "card-id", Ref: "card:work"}}
	if _, e := Enable(context.Background(), f.dir, f.b, f.now); e != nil {
		t.Fatal(e)
	}
	for _, ref := range []string{"work", "card-id", "card:card-id"} {
		in := f.entry()
		in.Card = ref
		f.write(in)
	}
	in := f.entry()
	f.write(in)
	f.c.Prepare = func(_ context.Context, b Binding, in Entry, _ time.Time) (integrations.Delivery, []string, error) {
		c, ok := b.Card(in.Card)
		if !ok || c.ID != "card-id" {
			t.Fatalf("wrong canonical card: %v %v", c, ok)
		}
		key := "anx-file-v1-" + b.ID + "-" + in.ID
		return integrations.Delivery{Key: key, PrincipalID: b.Actor, HostID: b.Host, BaseURL: b.BaseURL, Agent: b.Agent, CommandID: "events.create", Body: map[string]any{"request_key": key, "actor_id": b.Actor}}, nil, nil
	}
	s := f.tick()
	if s.Failed != 3 || len(f.calls) != 1 {
		t.Fatal(s, f.calls)
	}
	f.b.Cards[0].Ref = "card:work"
	if f.b.Validate() == nil {
		t.Fatal("duplicate public refs accepted")
	}
}

func TestMirrorExpiryUsesHostHeartbeatLifetime(t *testing.T) {
	f := newFixture(t)
	f.tick()
	if got := ExpiresAt(f.now); !got.Equal(f.now.Add(5 * bridge.HeartbeatInterval)) {
		t.Fatal(got)
	}
	if _, e := Inspect(f.dir, ExpiresAt(f.now).Add(-time.Nanosecond)); e != nil {
		t.Fatal(e)
	}
	if _, e := Inspect(f.dir, ExpiresAt(f.now)); e == nil {
		t.Fatal("expired mirror accepted")
	}
}

func TestHooksGuidePointsToShippedBridgeAndFileTransport(t *testing.T) {
	raw, e := os.ReadFile("../../docs/agent-hooks.md")
	if e != nil {
		t.Fatal(e)
	}
	for _, link := range []string{"(host-bridge.md)", "(file-transport.md)"} {
		if !strings.Contains(string(raw), link) {
			t.Fatalf("hooks guide lacks shipped integration link %s", link)
		}
	}
	if strings.Contains(string(raw), "belong to subsequent changes") {
		t.Fatal("hooks guide still defers shipped file transport")
	}
}

func TestMalformedPublishedFilenamesAreTerminal(t *testing.T) {
	for _, kind := range []string{"uuid-prefix", "oversize-name"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			in := f.entry()
			name := in.ID + ".extra.json"
			if kind == "oversize-name" {
				name = strings.Repeat("a", 140) + ".json"
				// A producer must not select a synthetic UUID by hashing a bad name.
				in.ID = claimID(name)
			}
			raw := integrations.JSON(in)
			if e := os.WriteFile(filepath.Join(f.project, ".anx/outbox", name), raw, 0600); e != nil {
				t.Fatal(e)
			}
			s := f.tick()
			if s.Failed != 1 || len(s.Pending) != 0 || len(f.calls) != 0 {
				t.Fatal(s, f.calls)
			}
			failed, e := os.ReadDir(filepath.Join(f.project, ".anx/outbox/.failed"))
			if e != nil || len(failed) != 2 {
				t.Fatal(failed, e)
			}
			for _, entry := range failed {
				b, e := os.ReadFile(filepath.Join(f.project, ".anx/outbox/.failed", entry.Name()))
				if e != nil {
					t.Fatal(e)
				}
				if strings.HasSuffix(entry.Name(), ".receipt.json") {
					if !strings.Contains(string(b), "invalid_filename") {
						t.Fatal(string(b))
					}
				} else if string(b) != string(raw) {
					t.Fatal("original bytes changed")
				}
			}
		})
	}
}
