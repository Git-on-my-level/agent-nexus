package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandVectorsRenderTogetherWithoutMutatingJSON(t *testing.T) {
	for _, key := range []string{"suggest", "argv", "next_argv", "workspace_next_argv"} {
		for _, args := range []any{[]string{"anx", "note", "card:ship", "two words", "--next", "<when>"}, []any{"anx", "note", "card:ship", "two words", "--next", "<when>"}} {
			e := Envelope{OK: true, Command: "other", Result: map[string]any{key: args}}
			var before, after, text bytes.Buffer
			WriteEnvelopeJSON(&before, e)
			if err := WriteEnvelopeText(&text, e); err != nil {
				t.Fatal(err)
			}
			WriteEnvelopeJSON(&after, e)
			if strings.Contains(text.String(), key+".0") || !strings.Contains(text.String(), "anx note card:ship 'two words' --next '<when>'") {
				t.Fatal(text.String())
			}
			if before.String() != after.String() {
				t.Fatalf("renderer mutated argv: %s", after.String())
			}
		}
	}
}
