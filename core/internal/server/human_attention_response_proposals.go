package server

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	humanAttentionResponseProposalMinCount = 1
	humanAttentionResponseProposalMaxCount = 6
	humanAttentionResponseProposalMaxRunes = 240
)

// NormalizeHumanAttentionResponseProposals validates payload.response_proposals for
// human_attention_requested: non-empty array of strings, 1–6 entries after trim,
// drop empties, exact-string dedupe (first wins), each at most 240 runes. No truncation.
func NormalizeHumanAttentionResponseProposals(raw any) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("is required")
	}
	list, err := coerceToAnySlice(raw)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("must be a non-empty array")
	}

	seen := make(map[string]struct{}, len(list))
	out := make([]string, 0, len(list))
	for idx, elem := range list {
		s, ok := elem.(string)
		if !ok {
			return nil, fmt.Errorf("entry %d must be a string", idx)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		if utf8.RuneCountInString(s) > humanAttentionResponseProposalMaxRunes {
			return nil, fmt.Errorf("entry exceeds %d characters", humanAttentionResponseProposalMaxRunes)
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	if len(out) < humanAttentionResponseProposalMinCount {
		return nil, fmt.Errorf("must have between %d and %d non-empty entries after normalization", humanAttentionResponseProposalMinCount, humanAttentionResponseProposalMaxCount)
	}
	if len(out) > humanAttentionResponseProposalMaxCount {
		return nil, fmt.Errorf("must have at most %d entries after normalization", humanAttentionResponseProposalMaxCount)
	}
	return out, nil
}

func coerceToAnySlice(raw any) ([]any, error) {
	switch v := raw.(type) {
	case []any:
		return v, nil
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be an array")
	}
}

// HumanAttentionResponseProposalsToAnySlice converts normalized strings to []any for JSON payloads.
func HumanAttentionResponseProposalsToAnySlice(proposals []string) []any {
	out := make([]any, len(proposals))
	for i, s := range proposals {
		out[i] = s
	}
	return out
}

func mutateHumanAttentionResponseProposalsInPayload(payload map[string]any) error {
	if payload == nil {
		return fmt.Errorf("event.payload is required")
	}
	raw, ok := payload["response_proposals"]
	if !ok || raw == nil {
		return fmt.Errorf("response_proposals is required")
	}
	normalized, err := NormalizeHumanAttentionResponseProposals(raw)
	if err != nil {
		return fmt.Errorf("response_proposals %w", err)
	}
	payload["response_proposals"] = HumanAttentionResponseProposalsToAnySlice(normalized)
	return nil
}
