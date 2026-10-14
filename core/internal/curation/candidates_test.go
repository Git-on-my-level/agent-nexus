package curation

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var candBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func candAt(h int) time.Time { return candBase.Add(time.Duration(h) * time.Hour) }

func TestKeyStableAndOrderIndependent(t *testing.T) {
	a := Key(ActionBatch, []string{"event:1", "event:2", "event:3"})
	b := Key(ActionBatch, []string{"event:3", "event:1", "event:2"})
	if a != b {
		t.Fatalf("order changed key: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "cur_") || len(a) != 4+24 {
		t.Fatalf("bad key shape: %s", a)
	}
	if Key(ActionMerge, []string{"event:1", "event:2", "event:3"}) == a {
		t.Fatal("action must affect key")
	}
	if Key(ActionBatch, []string{"event:1", "event:2"}) == a {
		t.Fatal("targets must affect key")
	}
	in := []string{"b", "a"}
	Key(ActionWithdraw, in)
	if in[0] != "b" {
		t.Fatal("Key mutated its input")
	}
}

func TestSimilarityCases(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		min, max float64
	}{
		{"identical", "Pick label scheme", "Pick label scheme", 1, 1},
		{"case and punctuation", "Pick label scheme!", "pick LABEL, scheme", 1, 1},
		{"reordered", "scheme label pick", "pick label scheme", 1, 1},
		{"stopwords differ", "Pick the label scheme", "Pick label scheme for", 1, 1},
		{"stopwords only", "the of and", "the of and", 0, 0},
		{"one empty", "pick label", "", 0, 0},
		{"unrelated", "Pick label scheme", "Rotate database credentials", 0, 0},
		{"partial", "Pick label scheme", "Pick color scheme", 0.5, 0.8},
		{"unicode equal", "Überprüfung Schlüssel 日本語", "überprüfung schlüssel 日本語", 1, 1},
		{"unicode unrelated", "日本語 テスト", "Überprüfung Schlüssel", 0, 0},
		{"single char tokens dropped", "x y z", "x y z", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Similarity(tc.a, tc.b)
			if got < tc.min || got > tc.max {
				t.Fatalf("Similarity(%q,%q)=%v want [%v,%v]", tc.a, tc.b, got, tc.min, tc.max)
			}
			if rev := Similarity(tc.b, tc.a); rev != got {
				t.Fatalf("not symmetric: %v vs %v", got, rev)
			}
		})
	}
}

func flagAll(refs []string) MootResult {
	return MootResult{Flagged: true, Sources: len(refs), Reason: fmt.Sprintf("all %d linked sources are done", len(refs))}
}

func TestMootCandidates(t *testing.T) {
	ask := AskInfo{ID: "a1", Title: "Pick label scheme?", ThreadID: "th1", SubjectRef: "card:abc", RelatedRefs: []string{"card:abc", "", "pr:x#1"}}
	tests := []struct {
		name string
		asks []AskInfo
		moot func([]string) MootResult
		want int
	}{
		{"flagged", []AskInfo{ask}, flagAll, 1},
		{"not flagged", []AskInfo{ask}, func([]string) MootResult { return MootResult{Sources: 2, Reason: "x"} }, 0},
		{"zero sources", []AskInfo{ask}, func([]string) MootResult { return MootResult{Flagged: true, Reason: "x"} }, 0},
		{"restricted", []AskInfo{func() AskInfo { a := ask; a.Restricted = true; return a }()}, flagAll, 0},
		{"proposal", []AskInfo{func() AskInfo { a := ask; a.Proposal = true; return a }()}, flagAll, 0},
		{"no refs", []AskInfo{{ID: "n", Title: "t"}}, flagAll, 0},
		{"nil func", []AskInfo{ask}, nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MootCandidates(tc.asks, tc.moot)
			if len(got) != tc.want {
				t.Fatalf("got %d candidates, want %d", len(got), tc.want)
			}
		})
	}

	got := MootCandidates([]AskInfo{ask}, flagAll)[0]
	if got.Action != ActionWithdraw || got.Targets[0] != "event:a1" || got.ThreadID != "th1" || got.SubjectRef != "card:abc" {
		t.Fatalf("unexpected candidate: %+v", got)
	}
	if got.Key != Key(ActionWithdraw, []string{"event:a1"}) || got.Score != 1 {
		t.Fatalf("bad key/score: %+v", got)
	}
	if got.Title != `Withdraw "Pick label scheme?"?` {
		t.Fatalf("title: %q", got.Title)
	}
	if got.Rationale != "Overtaken: all 2 linked sources are done." {
		t.Fatalf("rationale: %q", got.Rationale)
	}
	if got.ResponseProposals[0] != "Withdraw" || got.ResponseProposals[1] != "Keep" {
		t.Fatalf("responses: %v", got.ResponseProposals)
	}
}

func TestMootCandidatesDedupesRefs(t *testing.T) {
	var seen []string
	MootCandidates([]AskInfo{{ID: "a", SubjectRef: "card:1", RelatedRefs: []string{"card:1", "card:2", ""}}},
		func(refs []string) MootResult { seen = refs; return MootResult{} })
	if len(seen) != 2 || seen[0] != "card:1" || seen[1] != "card:2" {
		t.Fatalf("refs: %v", seen)
	}
}

func TestMootCandidatesPrivacy(t *testing.T) {
	ask := AskInfo{ID: "a1", Title: "Pick label scheme?", SubjectRef: "pr:secret-repo#5", RelatedRefs: []string{"card:secret-card"}}
	cases := map[string]func([]string) MootResult{
		"counts only": flagAll,
		"reason echoes ref": func(refs []string) MootResult {
			return MootResult{Flagged: true, Sources: 2, Reason: refs[0] + " is merged"}
		},
	}
	for name, moot := range cases {
		t.Run(name, func(t *testing.T) {
			got := MootCandidates([]AskInfo{ask}, moot)
			if len(got) != 1 {
				t.Fatalf("want 1, got %d", len(got))
			}
			for _, s := range []string{got[0].Title, got[0].Body, got[0].Rationale} {
				if strings.Contains(s, "secret") {
					t.Fatalf("leaked source ref: %q", s)
				}
			}
		})
	}
}

func TestMootCandidatesSortedByKey(t *testing.T) {
	var asks []AskInfo
	for i := 0; i < 8; i++ {
		asks = append(asks, AskInfo{ID: fmt.Sprintf("a%d", i), Title: "t", SubjectRef: "card:x"})
	}
	got := MootCandidates(asks, flagAll)
	for i := 1; i < len(got); i++ {
		if got[i-1].Key > got[i].Key {
			t.Fatal("not sorted by key")
		}
	}
}

func TestCandidatesCapAsks(t *testing.T) {
	var asks []AskInfo
	for i := 0; i < MaxAsks+5; i++ {
		asks = append(asks, AskInfo{ID: fmt.Sprintf("a%d", i), Title: "t", SubjectRef: "card:x"})
	}
	calls := 0
	got := MootCandidates(asks, func(refs []string) MootResult { calls++; return flagAll(refs) })
	if calls != MaxAsks || len(got) != MaxAsks {
		t.Fatalf("calls=%d got=%d want %d", calls, len(got), MaxAsks)
	}
	for _, c := range got {
		if c.Targets[0] == "event:a200" || c.Targets[0] == "event:a204" {
			t.Fatalf("beyond-cap ask considered: %v", c.Targets)
		}
	}
	// Batch: only first MaxAsks participate; 205 same-subject asks => 20 chunks of 10.
	b := BatchCandidates(asks)
	members := 0
	for _, c := range b {
		members += len(c.Targets)
	}
	if members != MaxAsks {
		t.Fatalf("batch members=%d want %d", members, MaxAsks)
	}
}

func TestDuplicateCandidatesCapCards(t *testing.T) {
	var cards []CardInfo
	for i := 0; i < MaxCards; i++ {
		cards = append(cards, CardInfo{Ref: fmt.Sprintf("card:u%d", i), Title: fmt.Sprintf("unique%dx topic%dy", i, i*7+1), Phase: "active", CreatedAt: candAt(i)})
	}
	cards = append(cards, CardInfo{Ref: "card:late1", Title: "late dup title", Phase: "active"}, CardInfo{Ref: "card:late2", Title: "late dup title", Phase: "active"})
	if got := DuplicateCandidates(cards, 0); len(got) != 0 {
		t.Fatalf("beyond-cap cards considered: %+v", got)
	}
}

func TestDuplicateCandidates(t *testing.T) {
	mk := func(ref, title, phase string, h int) CardInfo {
		return CardInfo{Ref: ref, Title: title, Phase: phase, CreatedAt: candAt(h)}
	}
	t.Run("merge keeps older", func(t *testing.T) {
		got := DuplicateCandidates([]CardInfo{
			mk("card:new", "Improve onboarding flow", "active", 5),
			mk("card:old", "Onboarding flow improve", "ready", 1),
		}, 0)
		if len(got) != 1 {
			t.Fatalf("got %d", len(got))
		}
		c := got[0]
		if c.Targets[0] != "card:new" || c.Targets[1] != "card:old" || c.SubjectRef != "card:old" || c.ThreadID != "" {
			t.Fatalf("targets: %+v", c)
		}
		if c.Title != `Merge "Improve onboarding flow" into "Onboarding flow improve"?` || c.Score != 1 {
			t.Fatalf("title/score: %q %v", c.Title, c.Score)
		}
		if c.ResponseProposals[0] != "Merge" || c.ResponseProposals[1] != "Keep both" || c.Action != ActionMerge {
			t.Fatalf("bad: %+v", c)
		}
	})
	t.Run("tie on time uses smaller ref", func(t *testing.T) {
		got := DuplicateCandidates([]CardInfo{mk("card:b", "same title here", "active", 1), mk("card:a", "same title here", "active", 1)}, 0)
		if len(got) != 1 || got[0].Targets[1] != "card:a" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("threshold", func(t *testing.T) {
		cards := []CardInfo{mk("card:a", "Pick label scheme", "active", 1), mk("card:b", "Pick color scheme", "active", 2)}
		if got := DuplicateCandidates(cards, 0); len(got) != 0 {
			t.Fatal("default threshold should reject partial match")
		}
		if got := DuplicateCandidates(cards, 0.5); len(got) != 1 {
			t.Fatal("lower threshold should accept")
		}
		if got := DuplicateCandidates(cards, -1); len(got) != 0 {
			t.Fatal("negative threshold uses default")
		}
	})
	t.Run("closed phases excluded", func(t *testing.T) {
		for _, ph := range []string{"done", "cancelled", "canceled", "archived", "resolved", "DONE"} {
			got := DuplicateCandidates([]CardInfo{mk("card:a", "same title here", ph, 1), mk("card:b", "same title here", "active", 2)}, 0)
			if len(got) != 0 {
				t.Fatalf("phase %s not excluded", ph)
			}
		}
	})
	t.Run("restricted excluded", func(t *testing.T) {
		r := mk("card:a", "same title here", "active", 1)
		r.Restricted = true
		if got := DuplicateCandidates([]CardInfo{r, mk("card:b", "same title here", "active", 2)}, 0); len(got) != 0 {
			t.Fatal("restricted card named")
		}
	})
	t.Run("each card once", func(t *testing.T) {
		got := DuplicateCandidates([]CardInfo{
			mk("card:a", "same title here", "active", 1),
			mk("card:b", "same title here", "active", 2),
			mk("card:c", "same title here", "active", 3),
			mk("card:d", "same title here", "active", 4),
		}, 0)
		used := map[string]int{}
		for _, c := range got {
			for _, r := range c.Targets {
				used[r]++
			}
		}
		if len(got) != 2 {
			t.Fatalf("want 2 merges, got %d", len(got))
		}
		for r, n := range used {
			if n != 1 {
				t.Fatalf("%s in %d candidates", r, n)
			}
		}
	})
	t.Run("greedy by highest score", func(t *testing.T) {
		got := DuplicateCandidates([]CardInfo{
			mk("card:a", "alpha beta gamma delta", "active", 1),
			mk("card:b", "alpha beta gamma delta epsilon", "active", 2),
			mk("card:c", "alpha beta gamma delta", "active", 3),
		}, 0.7)
		if len(got) != 1 || got[0].Score != 1 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("ref overlap", func(t *testing.T) {
		a := mk("card:a", "Alpha work", "active", 1)
		b := mk("card:b", "Totally different", "active", 2)
		a.RelatedRefs = []string{"pr:r#1", "pr:r#2", "doc:d1", "topic:t"}
		b.RelatedRefs = []string{"pr:r#1", "pr:r#2", "doc:d1", "topic:t"}
		got := DuplicateCandidates([]CardInfo{a, b}, 0)
		if len(got) != 1 || got[0].Score != 1 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("ref overlap needs two non-context", func(t *testing.T) {
		a := mk("card:a", "Alpha work", "active", 1)
		b := mk("card:b", "Totally different", "active", 2)
		a.RelatedRefs = []string{"pr:r#1", "doc:d1", "topic:t", "board:b", "thread:x"}
		b.RelatedRefs = []string{"pr:r#1", "doc:d1", "topic:t", "board:b", "thread:x"}
		if got := DuplicateCandidates([]CardInfo{a, b}, 0.1); len(got) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("long title truncated by runes", func(t *testing.T) {
		long := strings.Repeat("日本語", 100) + " shared words"
		got := DuplicateCandidates([]CardInfo{mk("card:a", long, "active", 1), mk("card:b", long, "active", 2)}, 0)
		if len(got) != 1 {
			t.Fatal("want 1")
		}
		if n := utf8.RuneCountInString(got[0].Title); n != MaxTitleRunes {
			t.Fatalf("runes=%d", n)
		}
		if !strings.HasSuffix(got[0].Title, "…") || !utf8.ValidString(got[0].Title) {
			t.Fatalf("bad truncation: %q", got[0].Title)
		}
	})
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("short", 10); got != "short" {
		t.Fatal(got)
	}
	if got := truncateRunes("日本語日本語", 4); got != "日本語…" {
		t.Fatal(got)
	}
	if got := truncateRunes("日本語日", 4); got != "日本語日" {
		t.Fatal(got)
	}
}

func TestBatchCandidates(t *testing.T) {
	mk := func(id, subj, thread string, h int) AskInfo {
		return AskInfo{ID: id, Title: "Ask " + id, SubjectRef: subj, ThreadID: thread, CreatedAt: candAt(h)}
	}
	t.Run("subject grouping", func(t *testing.T) {
		got := BatchCandidates([]AskInfo{
			mk("c", "card:1", "t3", 3), mk("a", "card:1", "t1", 1), mk("b", "card:1", "t2", 2), mk("z", "card:2", "t9", 4),
		})
		if len(got) != 1 {
			t.Fatalf("got %d", len(got))
		}
		c := got[0]
		want := []string{"event:a", "event:b", "event:c"}
		for i := range want {
			if c.Targets[i] != want[i] {
				t.Fatalf("targets %v", c.Targets)
			}
		}
		if c.SubjectRef != "card:1" || c.ThreadID != "t1" || c.Title != "Batch 3 related asks?" || c.Action != ActionBatch || c.Score != 1 {
			t.Fatalf("%+v", c)
		}
		if !strings.Contains(c.Body, "- Ask a\n") || !strings.Contains(c.Body, "- Ask c\n") {
			t.Fatalf("body: %q", c.Body)
		}
		if c.ResponseProposals[0] != "Batch" || c.ResponseProposals[1] != "Keep separate" {
			t.Fatal(c.ResponseProposals)
		}
	})
	t.Run("thread fallback", func(t *testing.T) {
		got := BatchCandidates([]AskInfo{mk("a", "", "t1", 1), mk("b", "", "t1", 2), mk("c", "", "t2", 3)})
		if len(got) != 1 || len(got[0].Targets) != 2 || got[0].ThreadID != "t1" || got[0].SubjectRef != "" {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("subject wins over thread", func(t *testing.T) {
		got := BatchCandidates([]AskInfo{mk("a", "card:1", "t1", 1), mk("b", "card:1", "t2", 2), mk("c", "", "t1", 3)})
		if len(got) != 1 || len(got[0].Targets) != 2 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("already batched excluded", func(t *testing.T) {
		a := mk("a", "card:1", "t1", 1)
		a.BatchID = "b1"
		if got := BatchCandidates([]AskInfo{a, mk("b", "card:1", "t1", 2)}); len(got) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("restricted and proposal excluded", func(t *testing.T) {
		a, p := mk("a", "card:1", "t1", 1), mk("p", "card:1", "t1", 2)
		a.Restricted, p.Proposal = true, true
		if got := BatchCandidates([]AskInfo{a, p, mk("b", "card:1", "t1", 3)}); len(got) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("singleton none", func(t *testing.T) {
		if got := BatchCandidates([]AskInfo{mk("a", "card:1", "t1", 1), mk("b", "card:2", "t1", 2), mk("c", "", "", 3)}); len(got) != 0 {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("no subject or thread never grouped", func(t *testing.T) {
		if got := BatchCandidates([]AskInfo{mk("a", "", "", 1), mk("b", "", "", 2)}); len(got) != 0 {
			t.Fatal("grouped orphan asks")
		}
	})
	chunk := []struct {
		n    int
		want []int
	}{
		{10, []int{10}},
		{11, []int{10}},
		{12, []int{10, 2}},
		{25, []int{10, 10, 5}},
	}
	for _, tc := range chunk {
		t.Run(fmt.Sprintf("chunk %d", tc.n), func(t *testing.T) {
			var asks []AskInfo
			for i := 0; i < tc.n; i++ {
				asks = append(asks, mk(fmt.Sprintf("a%02d", i), "card:1", "t", i))
			}
			got := BatchCandidates(asks)
			var sizes []int
			for _, c := range got {
				sizes = append(sizes, len(c.Targets))
			}
			if len(sizes) != len(tc.want) {
				t.Fatalf("sizes %v want %v", sizes, tc.want)
			}
			total := 0
			for _, s := range sizes {
				total += s
				if s > MaxBatchMembers {
					t.Fatalf("chunk too big: %d", s)
				}
			}
			wantTotal := 0
			for _, s := range tc.want {
				wantTotal += s
			}
			if total != wantTotal {
				t.Fatalf("total %d want %d", total, wantTotal)
			}
		})
	}
	t.Run("deterministic order", func(t *testing.T) {
		var asks []AskInfo
		for g := 0; g < 5; g++ {
			for i := 0; i < 2; i++ {
				asks = append(asks, mk(fmt.Sprintf("g%di%d", g, i), fmt.Sprintf("card:%d", g), "t", i))
			}
		}
		got := BatchCandidates(asks)
		if len(got) != 5 {
			t.Fatal(len(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i-1].Key > got[i].Key {
				t.Fatal("not sorted")
			}
		}
	})
}
