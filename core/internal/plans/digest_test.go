package plans

import (
	"fmt"
	"testing"
	"time"
)

func titles(list DigestList) []string {
	out := []string{}
	for _, step := range list.Items {
		out = append(out, step.Title)
	}
	return out
}

func TestDigestSplitsRecentCurrentAndNext(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	p := Plan{Steps: []Step{
		{ID: "design", Title: "Design", Ref: "card:design", After: []string{}},
		{ID: "build", Title: "Build", Ref: "card:build", After: []string{"design"}},
		{ID: "ship", Title: "Ship", Ref: "card:ship", After: []string{"build"}},
		{ID: "measure", Title: "Measure", After: []string{"ship"}},
		{ID: "write-up", Title: "Write it up", Status: "done", After: []string{}},
	}}
	facts := map[string]Fact{
		"card:design": {Known: true, Status: "done", MovementAt: now.Add(-30 * 24 * time.Hour)},
		"card:build":  {Known: true, Status: "done", MovementAt: now.Add(-36 * time.Hour)},
		"card:ship":   {Known: true, Status: "in_progress", MovementAt: now.Add(-2 * time.Hour)},
	}
	state := Compute(p, facts, now, now, 0)
	got := Digest(p, state, facts, now, 0, 0)

	if got.WindowHours != 168 {
		t.Fatalf("window %d", got.WindowHours)
	}
	// Design finished a month ago and is not news; the inline "done" step has no
	// completion time anywhere, so it is never claimed as recent.
	if want := []string{"Build"}; fmt.Sprint(titles(got.Completed)) != fmt.Sprint(want) {
		t.Fatalf("completed=%+v", got.Completed)
	}
	if got.Completed.Items[0].At != now.Add(-36*time.Hour).Format(time.RFC3339Nano) {
		t.Fatal(got.Completed.Items[0])
	}
	if want := []string{"Ship"}; fmt.Sprint(titles(got.Current)) != fmt.Sprint(want) {
		t.Fatalf("current=%+v", got.Current)
	}
	// Measure waits on Ship, so nothing is ready to start; a step that is
	// already active is current and must not be repeated as next.
	if len(got.Next.Items) != 0 || got.Next.More != 0 {
		t.Fatalf("next=%+v", got.Next)
	}
}

func TestDigestBoundsEveryListAndCountsTheRest(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	p := Plan{Steps: []Step{}}
	facts := map[string]Fact{}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("done-%d", i)
		p.Steps = append(p.Steps, Step{ID: id, Title: id, Ref: "card:" + id, After: []string{}})
		facts["card:"+id] = Fact{Known: true, Status: "done", MovementAt: now.Add(-time.Duration(i+1) * time.Hour)}
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("open-%d", i)
		p.Steps = append(p.Steps, Step{ID: id, Title: id, Ref: "card:" + id, After: []string{}})
		facts["card:"+id] = Fact{Known: true, Status: "in_progress"}
	}
	for i := 0; i < 5; i++ {
		p.Steps = append(p.Steps, Step{ID: fmt.Sprintf("todo-%d", i), Title: fmt.Sprintf("todo-%d", i), After: []string{}})
	}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	state := Compute(p, facts, now, now, 0)
	got := Digest(p, state, facts, now, 0, 0)

	for _, list := range []DigestList{got.Completed, got.Current, got.Next} {
		if len(list.Items) != StepDigestLimit || list.More != 2 {
			t.Fatalf("list=%+v", list)
		}
	}
	// Newest first.
	if got.Completed.Items[0].Title != "done-0" || got.Completed.Items[2].Title != "done-2" {
		t.Fatal(got.Completed)
	}
	// Plan order within a list, so two reads of one plan agree.
	if got.Current.Items[0].Title != "open-0" || got.Next.Items[0].Title != "todo-0" {
		t.Fatal(got.Current, got.Next)
	}
}

func TestDigestOfAnEmptyPlanListsNothing(t *testing.T) {
	now := time.Now().UTC()
	state := Compute(Plan{Steps: []Step{}}, nil, now, now, 0)
	got := Digest(Plan{Steps: []Step{}}, state, nil, now, 0, 0)
	for _, list := range []DigestList{got.Completed, got.Current, got.Next} {
		if list.Items == nil || len(list.Items) != 0 || list.More != 0 {
			t.Fatalf("list=%+v", list)
		}
	}
}

func TestDigestReportsBlockedStepsAsCurrent(t *testing.T) {
	now := time.Now().UTC()
	p := Plan{Steps: []Step{{ID: "a", Title: "A", Status: "blocked", After: []string{}}, {ID: "b", Title: "B", After: []string{"a"}}}}
	state := Compute(p, nil, now, now, 0)
	got := Digest(p, state, nil, now, 0, 0)
	if len(got.Current.Items) != 1 || got.Current.Items[0].Status != "blocked" {
		t.Fatalf("current=%+v", got.Current)
	}
	if len(got.Next.Items) != 0 {
		t.Fatalf("next=%+v", got.Next)
	}
}
