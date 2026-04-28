package app

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"agent-nexus-cli/internal/errnorm"
)

const (
	humanAttentionResponseProposalMinCount = 1
	humanAttentionResponseProposalMaxCount = 6
	humanAttentionResponseProposalMaxRunes = 240
)

// normalizeHumanAttentionResponseProposalsList applies the same rules as the core server:
// trim, drop empty, exact-string dedupe (first wins), 1–6 entries, ≤240 runes each.
func normalizeHumanAttentionResponseProposalsList(raw any) ([]string, error) {
	if raw == nil {
		return nil, fmt.Errorf("is required")
	}
	list, err := coerceHumanProposalSlice(raw)
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

func coerceHumanProposalSlice(raw any) ([]any, error) {
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

func humanAttentionResponseProposalsToAnySlice(proposals []string) []any {
	out := make([]any, len(proposals))
	for i, s := range proposals {
		out[i] = s
	}
	return out
}

func validatePreflightHumanAttentionResponseProposals(payload map[string]any) error {
	if payload == nil {
		return errnorm.Usage("invalid_request", "event.payload is required for event.type=\"human_attention_requested\"")
	}
	raw, ok := payload["response_proposals"]
	if !ok || raw == nil {
		return errnorm.Usage("invalid_request", "event.payload.response_proposals is required for event.type=\"human_attention_requested\"")
	}
	normalized, err := normalizeHumanAttentionResponseProposalsList(raw)
	if err != nil {
		return errnorm.Usage("invalid_request", fmt.Sprintf("event.payload.response_proposals: %v", err))
	}
	payload["response_proposals"] = humanAttentionResponseProposalsToAnySlice(normalized)
	return nil
}

// buildCLIHumanAttentionResponseProposals builds payload.response_proposals from
// --recommended-response and repeatable --proposal (alternatives only).
func buildCLIHumanAttentionResponseProposals(recommended string, alternatives []string) ([]any, error) {
	recommended = strings.TrimSpace(recommended)
	if recommended == "" {
		return nil, errnorm.Usage("invalid_request", "--recommended-response is required")
	}
	raw := []any{recommended}
	for _, alt := range alternatives {
		raw = append(raw, strings.TrimSpace(alt))
	}
	normalized, err := normalizeHumanAttentionResponseProposalsList(raw)
	if err != nil {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("response proposals: %v", err))
	}
	return humanAttentionResponseProposalsToAnySlice(normalized), nil
}
