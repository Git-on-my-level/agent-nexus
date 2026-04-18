package app

import (
	"fmt"
	"strings"
	"sync"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/registry"
)

var (
	embeddedEventRefRulesReg registry.EventRefRulesRegistry
	embeddedEventRefRulesErr error
	embeddedEventRefRulesOnce sync.Once
)

func loadEmbeddedEventRefRulesForPreflight() (registry.EventRefRulesRegistry, error) {
	embeddedEventRefRulesOnce.Do(func() {
		embeddedEventRefRulesReg, embeddedEventRefRulesErr = registry.LoadEmbeddedEventRefRules()
	})
	return embeddedEventRefRulesReg, embeddedEventRefRulesErr
}

func validateEventsCreateBody(body any) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	rawEvent, hasEvent := payload["event"]
	if !hasEvent {
		return nil
	}
	event, ok := rawEvent.(map[string]any)
	if !ok {
		return nil
	}
	eventType := strings.TrimSpace(anyString(event["type"]))
	if eventType == "" {
		return nil
	}

	rules, err := loadEmbeddedEventRefRulesForPreflight()
	if err != nil {
		return errnorm.Usage("invalid_request", fmt.Sprintf("internal: failed to load event reference rules: %v", err))
	}
	rule, known := rules.RuleForEventType(eventType)
	if !known {
		return nil
	}

	rawRefs, _ := event["refs"]
	refs, _ := asStringList(rawRefs)
	payloadMap := asMap(event["payload"])
	if payloadMap == nil {
		payloadMap = map[string]any{}
	}

	return validatePreflightEventRefRule(eventType, event, refs, payloadMap, rule)
}

func validatePreflightEventRefRule(eventType string, event map[string]any, refs []string, payload map[string]any, rule registry.EventRefRule) error {
	threadID, _ := event["thread_id"].(string)
	if strings.EqualFold(strings.TrimSpace(rule.ThreadID), "required") && strings.TrimSpace(threadID) == "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("event.thread_id is required for event.type=%q", eventType))
	}

	if err := validatePreflightRequiredRefPatterns(eventType, refs, rule.RefsMustInclude); err != nil {
		return err
	}

	if err := validatePreflightRequiredPayloadKeys(eventType, payload, rule.PayloadMustInclude); err != nil {
		return err
	}

	if err := validatePreflightConditionalRefRules(eventType, payload, refs, rule.ConditionalRefs); err != nil {
		return err
	}
	return nil
}

func validatePreflightRequiredRefPatterns(eventType string, refs []string, patterns []string) error {
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
		prefix := typedRefPrefix(ref)
		if prefix == "" {
			continue
		}
		actualByPrefix[prefix]++
	}

	for prefix, requiredCount := range requiredByPrefix {
		if actualByPrefix[prefix] >= requiredCount {
			continue
		}
		if requiredCount == 1 {
			return errnorm.Usage("invalid_request", fmt.Sprintf("event.refs must include a %q typed ref for event.type=%q", prefix+":<id>", eventType))
		}
		return errnorm.Usage("invalid_request", fmt.Sprintf("event.refs must include at least %d refs with prefix %q for event.type=%q", requiredCount, prefix, eventType))
	}
	return nil
}

func patternRefPrefix(pattern string) string {
	idx := strings.Index(pattern, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(pattern[:idx])
}

func validatePreflightRequiredPayloadKeys(eventType string, payload map[string]any, requiredKeys []string) error {
	for _, key := range requiredKeys {
		key = normalizePreflightRequiredPayloadKey(key)
		if key == "" {
			continue
		}
		value, exists := payload[key]
		if !exists || value == nil {
			return errnorm.Usage("invalid_request", fmt.Sprintf("event.payload.%s is required for event.type=%q", key, eventType))
		}
	}
	return nil
}

func normalizePreflightRequiredPayloadKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	idx := strings.IndexAny(key, " (\t")
	if idx <= 0 {
		return key
	}
	return strings.TrimSpace(key[:idx])
}

func validatePreflightConditionalRefRules(eventType string, payload map[string]any, refs []string, conditions []registry.ConditionalRef) error {
	if len(conditions) == 0 {
		return nil
	}

	prefixesPresent := make(map[string]bool)
	for _, ref := range refs {
		prefix := typedRefPrefix(ref)
		if prefix == "" {
			continue
		}
		prefixesPresent[prefix] = true
	}

	for _, cond := range conditions {
		payloadValue := getPreflightPayloadValue(payload, cond.When.PayloadField)
		if !strings.EqualFold(strings.TrimSpace(payloadValue), strings.TrimSpace(cond.When.Equals)) {
			continue
		}

		matchedCount := 0
		for _, req := range cond.MustHave {
			if prefixesPresent[strings.TrimSpace(req.Prefix)] {
				matchedCount++
			}
		}

		mode := strings.ToLower(strings.TrimSpace(cond.Condition))
		if mode == "or" {
			if matchedCount > 0 {
				continue
			}
			return errnorm.Usage("invalid_request", preflightConditionalRefErrorText(eventType, cond))
		}
		if len(cond.MustHave) > 0 && matchedCount != len(cond.MustHave) {
			return errnorm.Usage("invalid_request", preflightConditionalRefErrorText(eventType, cond))
		}
	}
	return nil
}

func getPreflightPayloadValue(payload map[string]any, fieldPath string) string {
	keys := strings.Split(fieldPath, ".")
	var current any = payload
	for _, key := range keys {
		if current == nil {
			return ""
		}
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	if v, ok := current.(string); ok {
		return v
	}
	return ""
}

func preflightConditionalRefErrorText(eventType string, cond registry.ConditionalRef) string {
	required := make([]string, len(cond.MustHave))
	for i, req := range cond.MustHave {
		required[i] = fmt.Sprintf("%s prefix", strings.TrimSpace(req.Prefix))
	}
	conditionText := strings.Join(required, " and ")
	if strings.TrimSpace(cond.Condition) == "or" {
		conditionText = strings.Join(required, " or ")
	}
	return fmt.Sprintf("event.refs must include %s when event.type=%q and payload.%s=%q",
		conditionText, eventType, cond.When.PayloadField, cond.When.Equals)
}
