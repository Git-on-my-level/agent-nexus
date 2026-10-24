package primitives

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"testing"

	contractenums "agent-nexus-contracts-go-client/enums"
)

var sqlPriorityWhen = regexp.MustCompile(`WHEN '([a-z0-9_]+)' THEN (\d+)`)

// The SQL rankings cannot import the generated enum (one backs an expression
// index), so pin every WHEN arm to it: each canonical value and alias ranks at
// its canonical position, nothing else is listed, and unknowns sort last.
func TestPrioritySQLMatchesContract(t *testing.T) {
	want := map[string]int{}
	for i, value := range contractenums.PriorityValues {
		want[value] = i
	}
	for alias, canonical := range contractenums.PriorityAliases {
		want[alias] = slices.Index(contractenums.PriorityValues, canonical)
	}
	for name, sql := range map[string]string{"briefPrioritySQL": briefPrioritySQL, "inboxAskPrioritySQL": inboxAskPrioritySQL} {
		got := map[string]int{}
		for _, m := range sqlPriorityWhen.FindAllStringSubmatch(sql, -1) {
			rank, _ := strconv.Atoi(m[2])
			got[m[1]] = rank
		}
		if len(got) != len(want) {
			keys := make([]string, 0, len(got))
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			t.Fatalf("%s ranks %v, want every priority value and alias %v", name, keys, want)
		}
		for value, rank := range want {
			if got[value] != rank {
				t.Fatalf("%s ranks %q at %d, contract says %d", name, value, got[value], rank)
			}
		}
		if !regexp.MustCompile(`ELSE ` + strconv.Itoa(len(contractenums.PriorityValues)) + ` END$`).MatchString(sql) {
			t.Fatalf("%s must sort unknown priorities last", name)
		}
	}
	for value, rank := range want {
		if briefPriority(value) != rank {
			t.Fatalf("briefPriority(%q)=%d, want %d", value, briefPriority(value), rank)
		}
	}
	if briefPriority("someday") != len(contractenums.PriorityValues) {
		t.Fatal("unknown priority must rank last")
	}
}
