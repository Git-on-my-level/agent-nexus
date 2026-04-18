package server

import (
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/schema"
)

func validatePacketArtifactAndContent(contract *schema.Contract, kind string, artifact map[string]any, packet map[string]any) (string, error) {
	if contract == nil {
		return "", fmt.Errorf("schema contract is required")
	}
	if artifact == nil {
		return "", fmt.Errorf("artifact is required")
	}
	if packet == nil {
		return "", fmt.Errorf("packet is required")
	}

	packetSchema, ok := contract.Packets[kind]
	if !ok {
		return "", fmt.Errorf("unsupported packet kind %q", kind)
	}

	for name, field := range packetSchema.Fields {
		value, exists := packet[name]
		if field.Required && !exists {
			return "", fmt.Errorf("packet.%s is required", name)
		}
		if !exists {
			continue
		}
		if err := validatePacketField(contract, name, value, field); err != nil {
			return "", err
		}
	}
	if legacyThreadID := strings.TrimSpace(anyString(packet["thread_id"])); legacyThreadID != "" {
		return "", fmt.Errorf("packet.thread_id is not supported; use packet.subject_ref")
	}

	idField, ok := packetIDFieldName(kind)
	if !ok {
		return "", fmt.Errorf("packet id rule is not defined for kind %q", kind)
	}
	packetID, ok := packet[idField].(string)
	if !ok || strings.TrimSpace(packetID) == "" {
		return "", fmt.Errorf("packet.%s is required", idField)
	}

	artifactID, _ := artifact["id"].(string)
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		artifactID = packetID
		artifact["id"] = artifactID
	}
	if artifactID != packetID {
		return "", fmt.Errorf("packet.%s must equal artifact.id", idField)
	}

	refs, err := extractStringSlice(artifact["refs"])
	if err != nil {
		return "", fmt.Errorf("artifact.refs must be a list of strings")
	}
	if err := schema.ValidateTypedRefs(contract, refs); err != nil {
		return "", err
	}

	if err := validateRequiredArtifactRefs(contract, kind, refs, packet); err != nil {
		return "", err
	}

	subjectRef, _ := packet["subject_ref"].(string)
	subjectRef = strings.TrimSpace(subjectRef)
	if subjectRef == "" {
		return "", fmt.Errorf("packet.subject_ref is required")
	}
	return subjectRef, nil
}

func validatePacketField(contract *schema.Contract, fieldName string, value any, spec schema.FieldSpec) error {
	switch spec.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("packet.%s must be a string", fieldName)
		}
		if spec.Required && strings.TrimSpace(text) == "" {
			return fmt.Errorf("packet.%s must be non-empty", fieldName)
		}
		if strings.HasPrefix(spec.Ref, "enums.") {
			enumName := strings.TrimPrefix(spec.Ref, "enums.")
			if err := schema.ValidateEnum(contract, enumName, text); err != nil {
				return fmt.Errorf("packet.%s: %w", fieldName, err)
			}
		}
	case "typed_ref":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("packet.%s must be a string", fieldName)
		}
		if err := schema.ValidateTypedRefs(contract, []string{text}); err != nil {
			return fmt.Errorf("packet.%s: %w", fieldName, err)
		}
	case "datetime":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("packet.%s must be an RFC3339 datetime string", fieldName)
		}
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return fmt.Errorf("packet.%s must be an RFC3339 datetime string", fieldName)
		}
	case "list<string>":
		values, err := extractStringSlice(value)
		if err != nil {
			return fmt.Errorf("packet.%s must be a list of strings", fieldName)
		}
		if spec.MinItems != nil && len(values) < *spec.MinItems {
			return fmt.Errorf("packet.%s must include at least %d item(s)", fieldName, *spec.MinItems)
		}
	case "list<typed_ref>":
		refs, err := extractStringSlice(value)
		if err != nil {
			return fmt.Errorf("packet.%s must be a list of strings", fieldName)
		}
		if fieldName == "outputs" && len(refs) == 0 {
			return fmt.Errorf("packet.%s must include at least 1 item(s)", fieldName)
		}
		if spec.MinItems != nil && len(refs) < *spec.MinItems {
			return fmt.Errorf("packet.%s must include at least %d item(s)", fieldName, *spec.MinItems)
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return fmt.Errorf("packet.%s: %w", fieldName, err)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("packet.%s must be an object", fieldName)
		}
	default:
		return fmt.Errorf("packet.%s has unsupported type %q", fieldName, spec.Type)
	}

	return nil
}

func validateRequiredArtifactRefs(contract *schema.Contract, kind string, refs []string, packet map[string]any) error {
	requiredTemplates := contract.ArtifactRefRules[kind]
	subjectRef := strings.TrimSpace(anyString(packet["subject_ref"]))
	if subjectRef == "" {
		return fmt.Errorf("packet.subject_ref is required")
	}

	for _, template := range requiredTemplates {
		if strings.Contains(template, "<topic_id> OR card:<card_id> OR board:<board_id>") {
			if !containsStringRef(refs, subjectRef) {
				return fmt.Errorf("artifact.refs must include %q", subjectRef)
			}
			continue
		}

		if strings.TrimSpace(template) == "card:<card_id>" {
			if !strings.HasPrefix(subjectRef, "card:") {
				return fmt.Errorf("packet.subject_ref must use card: prefix for kind %q", kind)
			}
			if !containsStringRef(refs, subjectRef) {
				return fmt.Errorf("artifact.refs must include %q", subjectRef)
			}
			continue
		}

		if strings.Contains(template, "<receipt_artifact_id>") {
			receiptRef := packetArtifactLinkRef(packet, "receipt_ref", "receipt_id")
			if receiptRef == "" || !containsStringRef(refs, receiptRef) {
				return fmt.Errorf("artifact.refs must include %q", receiptRef)
			}
			continue
		}

		if strings.Contains(template, "<artifact_id>") {
			artifactID, _ := packet[packetIDField(template, kind)].(string)
			artifactID = strings.TrimSpace(artifactID)
			expected := "artifact:" + artifactID
			if artifactID == "" || !containsStringRef(refs, expected) {
				return fmt.Errorf("artifact.refs must include %q", expected)
			}
			continue
		}
	}

	if !containsStringRef(refs, subjectRef) {
		return fmt.Errorf("artifact.refs must include %q", subjectRef)
	}

	return nil
}

func packetArtifactLinkRef(packet map[string]any, refField, idField string) string {
	if ref := strings.TrimSpace(anyString(packet[refField])); ref != "" {
		return ref
	}
	if id := strings.TrimSpace(anyString(packet[idField])); id != "" {
		return "artifact:" + id
	}
	return ""
}

func packetIDField(template, kind string) string {
	if strings.Contains(template, "<artifact_id>") {
		if idField, ok := packetIDFieldName(kind); ok {
			return idField
		}
	}
	return ""
}

func containsStringRef(refs []string, expected string) bool {
	for _, ref := range refs {
		if ref == expected {
			return true
		}
	}
	return false
}

func findFirstRefValueByPrefix(refs []string, prefix string) string {
	needle := prefix + ":"
	for _, ref := range refs {
		if !strings.HasPrefix(ref, needle) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(ref, needle))
		if value != "" {
			return value
		}
	}
	return ""
}

func packetIDFieldName(kind string) (string, bool) {
	switch kind {
	case "receipt":
		return "receipt_id", true
	case "review":
		return "review_id", true
	default:
		return "", false
	}
}
