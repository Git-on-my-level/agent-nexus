package skills

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var warningTriggerRe = regexp.MustCompile("`([^`]+)`")

// TestSkillOmitsWarningTriggers enforces invariant I6: the participant skill
// must not teach a rule that a write-time warning already delivers.
func TestSkillOmitsWarningTriggers(t *testing.T) {
	raw, err := os.ReadFile("../docs/warnings.md")
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 {
			continue
		}
		id := strings.TrimSpace(cells[0])
		if id == "" || strings.EqualFold(id, "id") || strings.Trim(id, "-: ") == "" {
			continue
		}
		m := warningTriggerRe.FindStringSubmatch(cells[2])
		if m == nil || strings.TrimSpace(m[1]) == "" {
			t.Errorf("warning %q lacks a backticked trigger phrase", id)
			continue
		}
		triggers = append(triggers, strings.TrimSpace(m[1]))
	}
	if len(triggers) < 8 {
		t.Fatalf("warnings.md lists %d triggers, want at least 8", len(triggers))
	}
	skill, err := os.ReadFile("anx-participant/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(skill))
	for _, trigger := range triggers {
		if strings.Contains(lower, strings.ToLower(trigger)) {
			t.Errorf("SKILL.md contains warning trigger phrase %q", trigger)
		}
	}
}
