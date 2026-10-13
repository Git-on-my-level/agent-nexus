package scopesearch

import (
	"encoding/json"
	"unicode/utf8"
)

// CommentProjection is the persisted, bounded input to the independent comment
// writer. Terms and text use the same Prepare kernel as the scoped executor.
// Authority is deliberately absent: the live adapter retains canonical event
// authorization, including inherited document and private-reference policy.
type CommentProjection struct {
	Preview   string   `json:"preview"`
	Text      string   `json:"text"`
	Terms     []string `json:"terms"`
	Truncated bool     `json:"truncated"`
}

func PrepareComment(input string) CommentProjection {
	truncated := len(input) > MaxTextBytes
	if truncated {
		end := MaxTextBytes
		for end > 0 && !utf8.RuneStart(input[end]) {
			end--
		}
		input = input[:end]
	}
	text := Prepare("", input)
	terms := text.Terms()
	if terms == nil {
		terms = []string{}
	}
	preview := input
	if len(preview) > MaxSnippetBytes {
		end := MaxSnippetBytes
		for end > 0 && !utf8.RuneStart(preview[end]) {
			end--
		}
		preview = preview[:end]
	}
	return CommentProjection{Preview: preview, Text: text.Normalized(), Terms: terms, Truncated: truncated || text.Truncated()}
}

func EncodeComment(input string) (string, error) {
	b, err := json.Marshal(PrepareComment(input))
	return string(b), err
}
