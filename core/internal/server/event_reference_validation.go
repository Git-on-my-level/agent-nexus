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

	if err := validateConditionalRefRules(eventType, payload, refs, rule.ConditionalRefs); err != nil {
		return err
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

func validateConditionalRefRules(eventType string, payload map[string]any, refs []string, conditions []schema.ConditionalRefRule) error {
	if len(conditions) == 0 {
		return nil
	}

	prefixesPresent := make(map[string]bool)
	for _, ref := range refs {
		prefix, _, err := schema.SplitTypedRef(ref)
		if err != nil {
			continue
		}
		prefixesPresent[prefix] = true
	}

	for _, cond := range conditions {
		payloadValue := getPayloadValue(payload, cond.When.PayloadField)
		if !strings.EqualFold(strings.TrimSpace(payloadValue), cond.When.Equals) {
			continue
		}

		hasRequired := false
		for _, req := range cond.MustHave {
			if prefixesPresent[req.Prefix] {
				hasRequired = true
				break
			}
		}

		if cond.Condition == "or" && !hasRequired {
			return fmtConditionalRefError(eventType, cond)
		}
		if cond.Condition != "or" && len(cond.MustHave) > 0 && !hasRequired {
			return fmtConditionalRefError(eventType, cond)
		}
	}

	return nil
}

func getPayloadValue(payload map[string]any, fieldPath string) string {
	keys := strings.Split(fieldPath, ".")
	var current any = payload

	for _, key := range keys {
		if current == nil {
			return ""
		}
		if m, ok := current.(map[string]any); ok {
			current = m[key]
		} else {
			return ""
		}
	}

	if v, ok := current.(string); ok {
		return v
	}
	return ""
}

func fmtConditionalRefError(eventType string, cond schema.ConditionalRefRule) error {
	required := make([]string, len(cond.MustHave))
	for i, req := range cond.MustHave {
		required[i] = fmt.Sprintf("%s prefix", req.Prefix)
	}

	conditionText := strings.Join(required, " and ")
	if cond.Condition == "or" {
		conditionText = strings.Join(required, " or ")
	}

	return fmt.Errorf("event.refs must include %s when event.type=%q and payload.%s=%q",
		conditionText, eventType, cond.When.PayloadField, cond.When.Equals)
}

func patternRefPrefix(pattern string) string {
	idx := strings.Index(pattern, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(pattern[:idx])
}
