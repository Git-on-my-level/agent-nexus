package app

import (
	"strings"
	"testing"
)

func TestTopicWorkspaceTextTruncation(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		body := map[string]any{"topic": map[string]any{"id": "topic-1", "title": "Dense topic"}, "cards_truncated": truncated, "resources_truncated": truncated}
		text := formatTopicWorkspace(body)
		for _, field := range []string{"cards_truncated", "resources_truncated"} {
			want := field + ": false"
			if truncated {
				want = field + ": true"
			}
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q in %s", want, text)
			}
		}
		if truncated {
			for _, route := range []string{"/cards", "/boards", "/documents", "/threads", "/inbox"} {
				if !strings.Contains(text, route) {
					t.Fatalf("missing route %s: %s", route, text)
				}
			}
		} else if strings.Contains(text, "Bounded workspace preview") {
			t.Fatalf("unexpected truncation guidance: %s", text)
		}
	}
}
