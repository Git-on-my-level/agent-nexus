package scopeboundary_test

import (
	b "agent-nexus-core/experiments/scopeboundary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func open(t *testing.T, path string) *b.Model { t.Helper(); m, e := b.Open(path); must(t, e); return m }
func TestLifecycleFence(t *testing.T) {
	for _, n := range []int{1000, 10000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "model.sqlite")
			m := open(t, path)
			defer func() { m.DB.Close() }()
			tx, e := m.DB.Begin()
			must(t, e)
			_, e = tx.Exec(`INSERT INTO scopes(id) VALUES(1);INSERT INTO resources VALUES(1,1,0,'parent',0);INSERT INTO counters VALUES(1,?)`, n+1)
			must(t, e)
			for i := 2; i <= n+2; i++ {
				parent := 1
				if i == n+2 {
					parent = 0
				}
				_, e = tx.Exec(`INSERT INTO resources VALUES(?,1,?,'child',0)`, i, parent)
				must(t, e)
				_, e = tx.Exec(`INSERT INTO feed VALUES(1,'work','all',?,?,?)`, i, i, parent)
				must(t, e)
			}
			must(t, tx.Commit())
			var before, after int
			must(t, m.DB.QueryRow(`SELECT total_changes()`).Scan(&before))
			must(t, m.Archive(1, 1))
			must(t, m.DB.QueryRow(`SELECT total_changes()`).Scan(&after))
			if after-before != 3 {
				t.Fatalf("archive fanout %d", after-before)
			}
			if _, e = m.Count(1); !errors.Is(e, b.ErrUpdating) {
				t.Fatal(e)
			}
			if _, e = m.Page(1, 1); !errors.Is(e, b.ErrUpdating) {
				t.Fatal(e)
			}
			steps := 0
			for {
				count, done, e := m.Step(1, 64)
				must(t, e)
				if count > 64 {
					t.Fatal("unbounded worker")
				}
				steps++
				if steps == 2 {
					must(t, m.DB.Close())
					m = open(t, path)
				}
				if done {
					break
				}
			}
			count, e := m.Count(1)
			must(t, e)
			if count != 1 {
				t.Fatal(count)
			}
			b.Visits.Store(0)
			rows, e := m.Page(1, 1)
			must(t, e)
			if len(rows) != 1 || rows[0] != n+2 || b.Visits.Load() != 1 {
				t.Fatalf("rows %v visits %d", rows, b.Visits.Load())
			}
			t.Logf("descendants=%d archive_writes=3 worker_slices=%d page_examined=%d count=%d", n, steps, b.Visits.Load(), count)
		})
	}
}
func TestAudienceStreams64Scopes(t *testing.T) {
	m := open(t, filepath.Join(t.TempDir(), "m.sqlite"))
	defer m.DB.Close()
	tx, e := m.DB.Begin()
	must(t, e)
	for scope := 1; scope <= 64; scope++ {
		_, e = tx.Exec(`INSERT INTO scopes(id) VALUES(?);INSERT INTO grants VALUES('reader',?)`, scope, scope)
		must(t, e)
		for i := 1; i <= 1000; i++ {
			_, e = tx.Exec(`INSERT INTO changes VALUES(?,'inbox','person:stranger',?,?)`, scope, i, fmt.Sprint(i))
			must(t, e)
		}
		_, e = tx.Exec(`INSERT INTO changes VALUES(?,'inbox','person:reader',1,'visible')`, scope)
		must(t, e)
	}
	for scope := 65; scope <= 10064; scope++ {
		_, e = tx.Exec(`INSERT INTO scopes(id,state) VALUES(?,'sealed');INSERT INTO grants VALUES('reader',?)`, scope, scope)
		must(t, e)
	}
	must(t, tx.Commit())
	var streams []b.Stream
	for scope := 1; scope <= 64; scope++ {
		streams = append(streams, b.Stream{Scope: scope, Family: "inbox", Audience: "person:reader"})
	}
	b.Visits.Store(0)
	out, e := m.Poll("reader", streams, 1)
	must(t, e)
	if len(out) != 1 || out[0] != "visible" || b.Visits.Load() != 64 {
		t.Fatalf("%v visits%d", out, b.Visits.Load())
	}
	t.Logf("scopes=64 wrong_audience_changes=64000 examined=%d", b.Visits.Load())
	// A one-row tick advances only its emitted stream; every other head survives.
	_, next, e := m.PollPage("reader", streams, 1)
	must(t, e)
	advanced := 0
	for _, stream := range next {
		if stream.After != 0 {
			advanced++
		}
	}
	if advanced != 1 {
		t.Fatal("dropped un-emitted stream heads", next)
	}
	delivered := 1
	for delivered < 64 {
		rows, cursor, e := m.PollPage("reader", next, 1)
		must(t, e)
		if len(rows) != 1 {
			t.Fatal("lost stream continuation")
		}
		next = cursor
		delivered++
	}
	streams = next

	b.Visits.Store(0)
	out, e = m.Poll("reader", streams, 1)
	must(t, e)
	if len(out) != 0 || b.Visits.Load() != 0 {
		t.Fatal("idle tick read wrong audiences")
	}
	b.Visits.Store(0)
	directory, e := m.Directory("reader", 64)
	must(t, e)
	if len(directory) != 65 || b.Visits.Load() != 65 {
		t.Fatalf("directory %d visits%d", len(directory), b.Visits.Load())
	}
	t.Logf("sealed_grants=10000 directory_examined=%d; idle_tick_examined=0", b.Visits.Load())
	oversized := make([]b.Stream, 257)
	if _, e = m.Poll("reader", oversized, 1); !errors.Is(e, b.ErrBudget) {
		t.Fatal("stream budget missing", e)
	}
	var full []b.Stream
	for scope := 1; scope <= 64; scope++ {
		for _, family := range []string{"inbox", "work"} {
			for _, audience := range []string{"all", "person:reader"} {
				full = append(full, b.Stream{Scope: scope, Family: family, Audience: audience})
			}
		}
	}
	_, e = m.Poll("reader", full, 1)
	must(t, e)
	if _, e = m.Poll("reader", append(full[:4:4], b.Stream{Scope: 1, Family: "extra", Audience: "all"}), 1); !errors.Is(e, b.ErrBudget) {
		t.Fatal("per-scope stream bound missing", e)
	}
	if _, e = m.Poll("reader", []b.Stream{full[0], full[0]}, 1); !errors.Is(e, b.ErrBudget) {
		t.Fatal("duplicate stream accepted", e)
	}
	streams = append(streams, b.Stream{Scope: 65, Family: "inbox", Audience: "all"})
	if _, e = m.Poll("reader", streams, 1); !errors.Is(e, b.ErrBudget) {
		t.Fatal(e)
	}
	streams = streams[:64]
	streams[0].Audience = "person:stranger"
	if _, e = m.Poll("reader", streams, 1); e == nil {
		t.Fatal("accepted wrong audience")
	}
}
func TestIdentityAllocationDifferential(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		m := open(t, filepath.Join(t.TempDir(), "m.sqlite"))
		_, e := m.DB.Exec(`INSERT INTO grants VALUES('creator',1)`)
		must(t, e)
		if hidden {
			_, e = m.DB.Exec(`INSERT INTO resources VALUES(1,2,0,'private report',0)`)
			must(t, e)
			_, e = m.DB.Exec(`INSERT INTO aliases VALUES(2,'report','private',0),(3,'report','old-private',1)`)
			must(t, e)
		}
		id, e := m.Create("creator", "replay", "report", 1)
		must(t, e)
		if len(id) != 32 {
			t.Fatal(id)
		}
		again, e := m.Create("creator", "replay", "report", 1)
		must(t, e)
		if again != id {
			t.Fatal("replay changed")
		}
		if _, e = m.Create("creator", "replay", "different", 1); e == nil {
			t.Fatal("mismatched replay accepted")
		}

		var alias string
		must(t, m.DB.QueryRow(`SELECT name FROM aliases WHERE scope=1`).Scan(&alias))
		if alias != "report" {
			t.Fatal("hidden suffix leak")
		}
		must(t, m.DB.Close())
		t.Logf("hidden_alias_and_tombstone=%t status=success alias=report opaque_id_bytes=16", hidden)
	}
}
func TestDerivationRejectsPrivilegedCopy(t *testing.T) {
	m := open(t, filepath.Join(t.TempDir(), "m.sqlite"))
	defer m.DB.Close()
	_, e := m.DB.Exec(`INSERT INTO grants VALUES('pm',1),('pm',2);INSERT INTO resources VALUES(1,2,0,'PRIVATE SECRET',0)`)
	must(t, e)
	private, e := m.Compute("pm", 2)
	must(t, e)
	public, e := m.Compute("pm", 1)
	must(t, e)
	v, e := private.Title(1)
	must(t, e)
	for _, e := range []error{private.Persist(1, "direct", v), public.Persist(1, "rebound", v), private.Persist(1, "implicit", private.Constant("one private result"))} {
		if !errors.Is(e, b.ErrDerivation) {
			t.Fatal("copy accepted", e)
		}
	}
	if strings.Contains(fmt.Sprintf("%#v %#v", v, private), "PRIVATE SECRET") {
		t.Fatal("handle contains plaintext")
	}
	must(t, private.Persist(2, "positive", v))
	var n int
	must(t, m.DB.QueryRow(`SELECT count(*) FROM projections WHERE scope=1`).Scan(&n))
	if n != 0 {
		t.Fatal(n)
	}
	t.Log("owner/PM reads private and writes public: direct, rebound and scope-pinned constants rejected; same-scope positive passes")
}
func TestBoundedOrderingAndCommentWrites(t *testing.T) {
	m := open(t, filepath.Join(t.TempDir(), "m.sqlite"))
	defer m.DB.Close()
	tx, e := m.DB.Begin()
	must(t, e)
	for i := 1; i <= 10000; i++ {
		_, e = tx.Exec(`INSERT INTO ordering VALUES(1,'ready',?,?);INSERT INTO comments VALUES(?,7,'old history')`, i*1024, i, i)
		must(t, e)
	}
	_, e = tx.Exec(`INSERT INTO ordering VALUES(1,'dense',1,1),(1,'dense',2,2)`)
	must(t, e)
	must(t, tx.Commit())
	b.Visits.Store(0)
	rank, e := m.RankBetween(1, "ready", 5000*1024)
	must(t, e)
	if rank != 5000*1024+512 || b.Visits.Load() != 1 {
		t.Fatal(rank, b.Visits.Load())
	}
	if _, e = m.RankBetween(1, "dense", 1); e == nil {
		t.Fatal("rank exhaustion silently rebalanced")
	}
	var before, after int
	must(t, m.DB.QueryRow(`SELECT total_changes()`).Scan(&before))
	must(t, m.AppendComment(10001, 7, "new bounded comment"))
	must(t, m.DB.QueryRow(`SELECT total_changes()`).Scan(&after))
	if after-before != 4 {
		t.Fatal(after - before)
	}
	t.Log("column=10000 successor_examined=1; exhausted gap refuses; thread_history=10000 comment_write_rows=4")
}
func TestOversizedSearchProgress(t *testing.T) {
	large := strings.Repeat("x", b.SearchBytes*2) + "needle"
	candidates := []b.Candidate{{ID: 1, Version: 1, Body: large}, {ID: 2, Version: 1, Body: "needle"}}
	cursor := b.SearchCursor{Generation: 9}
	out, next, e := b.Search(candidates, "needle", cursor, 9, b.SearchBytes)
	must(t, e)
	if len(out) != 0 || next.Index != 1 {
		t.Fatal(out, next)
	}
	out, last, e := b.Search(candidates, "needle", next, 9, b.SearchBytes)
	must(t, e)
	if len(out) != 1 || out[0] != 2 || last.Index != 2 {
		t.Fatal(out, last)
	}
	if _, _, e = b.Search(candidates, "needle", next, 10, b.SearchBytes); e == nil {
		t.Fatal("stale snapshot accepted")
	}
	t.Log("oversized candidate coverage=65536 bytes; later match explicitly outside coverage; next candidate reached; stale generation restarts")
}
