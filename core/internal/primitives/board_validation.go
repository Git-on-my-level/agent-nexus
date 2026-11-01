// Validation and normalization for board and card request fields: ids,
// client text, typed refs, resolutions, and the invalid-board-request errors.

package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/schema"
)

func validateBoardCardClientText(field, value string) error {
	if strings.ContainsRune(value, 0) || !utf8.ValidString(value) {
		return invalidBoardRequest(field + " must be valid UTF-8 text without NUL")
	}
	return nil
}

func validateBoardCardTypedRefs(field string, refs []string) error {
	for i, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, _, err := schema.SplitTypedRef(ref); err != nil {
			return invalidBoardRequest(fmt.Sprintf("%s[%d] %s", field, i, err.Error()))
		}
	}
	return nil
}

// classifyBoardCardRequestError turns client mistakes that surface inside the
// card-create transaction into the sentinels the HTTP layer already maps to
// 4xx. Other failures stay untouched so the outcome-unknown wrap still applies.
func classifyBoardCardRequestError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
		if errors.Is(err, ErrInvalidBoardRequest) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrInvalidBoardRequest, err)
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	default:
		return err
	}
}

func validateBoardID(boardID string) error {
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return fmt.Errorf("board.id is required")
	}
	if strings.Contains(boardID, "/") {
		return fmt.Errorf("board.id contains invalid path characters")
	}
	return nil
}

func validateCardID(cardID string) error {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return fmt.Errorf("card.id is required")
	}
	if strings.Contains(cardID, "/") || strings.Contains(cardID, `\`) {
		return fmt.Errorf("card.id contains invalid path characters")
	}
	return nil
}

func validateThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("thread_id is required")
	}
	if strings.Contains(threadID, "/") || strings.Contains(threadID, `\`) {
		return fmt.Errorf("thread_id contains invalid path characters")
	}
	return nil
}

func canonicalizeCardResolutionForAPI(raw string) any {
	s := canonicalizeStoredCardResolution(raw)
	if s == "" {
		return nil
	}
	switch s {
	case "done":
		return s
	default:
		return nil
	}
}

func canonicalizeStoredCardResolution(raw string) string {
	s := strings.TrimSpace(raw)
	switch s {
	case "completed", "superseded":
		return "done"
	case "unresolved":
		return ""
	default:
		return s
	}
}

// Incoming writes are contract-strict. Legacy aliases are handled only when
// shaping already-persisted rows through canonicalizeStoredCardResolution.
func normalizeIncomingCardResolution(raw string) string {
	return strings.TrimSpace(raw)
}

func validateCardResolution(raw string, allowEmpty bool) error {
	value := normalizeIncomingCardResolution(raw)
	if value == "" && allowEmpty {
		return nil
	}
	switch value {
	case "done":
		return nil
	default:
		return fmt.Errorf("card.resolution must be null or done")
	}
}

func invalidBoardRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidBoardRequest, strings.TrimSpace(message))
}

func invalidBoardRequestError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidBoardRequest, strings.TrimSpace(err.Error()))
}
