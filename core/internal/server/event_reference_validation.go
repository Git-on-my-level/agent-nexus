package server

import (
	"fmt"
	"strings"

	"organization-autorunner-core/internal/schema"
)

func validateEventReferenceConventions(contract *schema.Contract, event map[string]any, refs []string) error {
	if contract == nil {
		return fmt.Errorf("schema contract is required")
	}

	eventType, _ := event["type"].(string)
	rule, known := contract.EventRefRules[eventType]
	if !known {
		// Unknown/open event types are allowed without convention checks.
		return nil
	}

	threadID, _ := event["thread_id"].(string)
	if strings.EqualFold(strings.TrimSpace(rule.ThreadID), "required") && strings.TrimSpace(threadID) == "" {
		return fmt.Errorf("event.thread_id is required for event.type=%q", eventType)
	}

	if err := validateRequiredRefPatterns(eventType, refs, rule.RefsMustInclude); err != nil {
		return err
	}

	payload := map[string]any{}
	if rawPayload, exists := event["payload"]; exists && rawPayload != nil {
		if parsed, ok := rawPayload.(map[string]any); ok {
			payload = parsed
		}
	}

	if err := validateRequiredPayloadKeys(eventType, payload, rule.PayloadMustInclude); err != nil {
		return err
	}

	if eventType == "commitment_status_changed" {
		if err := validateCommitmentStatusChangedRefs(payload, refs); err != nil {
			return err
		}
	}

	return nil
}

func validateRequiredRefPatterns(eventType string, refs []string, patterns []string) error {
	if len(patterns) == 0 {
		return nil
	}

	requiredByPrefix := make(map[string]int)
	for _, pattern := range patterns {
		prefix := patternRefPrefix(pattern)
		if prefix == "" {
			continue
		}
		requiredByPrefix[prefix]++
	}

	actualByPrefix := make(map[string]int)
	for _, ref := range refs {
		prefix, _, err := schema.SplitTypedRef(ref)
		if err != nil {
			continue
		}
		actualByPrefix[prefix]++
	}

	for prefix, requiredCount := range requiredByPrefix {
		if actualByPrefix[prefix] >= requiredCount {
			continue
		}
		if requiredCount == 1 {
			return fmt.Errorf("event.refs must include a %q typed ref for event.type=%q", prefix+":<id>", eventType)
		}
		return fmt.Errorf("event.refs must include at least %d refs with prefix %q for event.type=%q", requiredCount, prefix, eventType)
	}

	return nil
}

func validateRequiredPayloadKeys(eventType string, payload map[string]any, requiredKeys []string) error {
	for _, key := range requiredKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value, exists := payload[key]
		if !exists || value == nil {
			return fmt.Errorf("event.payload.%s is required for event.type=%q", key, eventType)
		}
	}
	return nil
}

func validateCommitmentStatusChangedRefs(payload map[string]any, refs []string) error {
	status := commitmentTargetStatus(payload)
	if status == "" {
		return nil
	}

	hasArtifactRef := false
	hasEventRef := false
	for _, ref := range refs {
		prefix, _, err := schema.SplitTypedRef(ref)
		if err != nil {
			continue
		}
		if prefix == "artifact" {
			hasArtifactRef = true
		}
		if prefix == "event" {
			hasEventRef = true
		}
	}

	switch status {
	case "done":
		if hasArtifactRef || hasEventRef {
			return nil
		}
		return fmt.Errorf("event.refs must include artifact:<receipt_id> or event:<decision_event_id> when event.type=\"commitment_status_changed\" and payload.to_status=\"done\"")
	case "canceled":
		if hasEventRef {
			return nil
		}
		return fmt.Errorf("event.refs must include event:<decision_event_id> when event.type=\"commitment_status_changed\" and payload.to_status=\"canceled\"")
	default:
		return nil
	}
}

func commitmentTargetStatus(payload map[string]any) string {
	if toStatus, ok := payload["to_status"].(string); ok {
		return strings.TrimSpace(toStatus)
	}
	if status, ok := payload["status"].(string); ok {
		return strings.TrimSpace(status)
	}
	return ""
}

func patternRefPrefix(pattern string) string {
	idx := strings.Index(pattern, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(pattern[:idx])
}
