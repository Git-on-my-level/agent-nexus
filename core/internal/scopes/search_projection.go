package scopes

import (
	"strings"
	"unicode/utf8"
)

// BoundSearchProjection prepares an incoming current-head/comment delta for the
// trusted canonical hook. It does not load history, grant authority or publish.
// Non-body metadata must already fit the shared budget. It inspects/copies at
// most MaxValueBytes of body, retains a complete UTF-8 prefix, and carries any
// earlier source truncation forward. The input may be the incoming large body;
// callers must not pretruncate it without setting SourceTruncated.
func BoundSearchProjection(source Projection) (Projection, error) {
	metadata := len(source.Title) + len(source.ContainerID) + len(source.Status) + len(source.Rank)
	if metadata > MaxValueBytes || !utf8.ValidString(source.Title) {
		return Projection{}, ErrBudget
	}
	available := MaxValueBytes - metadata
	if len(source.Text) > available {
		source.SourceTruncated = true
		end := available
		// The first omitted byte may be inside a rune. Never retain a partial rune.
		for end > 0 && !utf8.RuneStart(source.Text[end]) {
			end--
		}
		source.Text = source.Text[:end]
	}
	if !utf8.ValidString(source.Text) {
		return Projection{}, ErrBudget
	}
	// Detach bounded strings so a short prefix cannot retain an oversized input.
	source.Title = strings.Clone(source.Title)
	source.Text = strings.Clone(source.Text)
	source.ContainerID = strings.Clone(source.ContainerID)
	source.Status = strings.Clone(source.Status)
	source.Rank = strings.Clone(source.Rank)
	return source, nil
}
