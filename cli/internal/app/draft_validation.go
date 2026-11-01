// Validation of draft request bodies per draft command, including the
// per-resource field validators and packet-for-kind checks invoked at commit.

package app

import (
	"fmt"
	"strings"

	contractsclient "agent-nexus-contracts-go-client/client"
	contractenums "agent-nexus-contracts-go-client/enums"

	"agent-nexus-cli/internal/errnorm"
)

func resolveDraftCommandID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errnorm.Usage("invalid_request", "command is required; use --command <command-id>")
	}
	if strings.Contains(raw, ".") {
		spec, ok := commandSpecByID(raw)
		if !ok {
			if _, fallback := draftCommandFallbackSpecs[raw]; fallback {
				return raw, nil
			}
			return "", errnorm.Usage("invalid_request", fmt.Sprintf("unknown command id %q", raw))
		}
		return spec.CommandID, nil
	}
	registryPath := mapRuntimePathToRegistryPath(raw)
	for _, spec := range contractsclient.CommandRegistry {
		if strings.TrimSpace(spec.CLIPath) == registryPath {
			return strings.TrimSpace(spec.CommandID), nil
		}
	}
	if _, ok := draftCommandFallbackSpecs[registryPath]; ok {
		return registryPath, nil
	}
	return "", errnorm.Usage("invalid_request", fmt.Sprintf("unknown command %q", raw))
}

func validateDraftBody(commandID string, body map[string]any) []string {
	spec, ok := draftCommandSpecByID(commandID)
	if !ok {
		return []string{fmt.Sprintf("unknown command id %q", commandID)}
	}
	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	if method != "POST" && method != "PATCH" && method != "PUT" {
		return []string{fmt.Sprintf("command %q is read-only and cannot be staged as a draft", commandID)}
	}
	if strings.TrimSpace(spec.InputMode) == "" || strings.TrimSpace(spec.InputMode) == "none" {
		return []string{fmt.Sprintf("command %q does not accept a request body", commandID)}
	}
	validators := map[string]func(map[string]any) []string{
		"topics.create":         validateDraftTopicCreate,
		"topics.patch":          validateDraftTopicPatch,
		"cards.patch":           validateDraftCardPatch,
		"cards.move":            validateDraftCardMove,
		"docs.revisions.create": validateDraftDocsRevision,
		"events.create":         validateDraftEventCreate,
		"artifacts.create":      validateDraftArtifactCreate,
		"inbox.respond":         validateDraftInboxRespond,
		"derived.rebuild":       validateDraftDerivedRebuild,
	}
	validate, exists := validators[commandID]
	if !exists {
		return []string{fmt.Sprintf("command %q is not yet supported by draft create", commandID)}
	}
	return validate(body)
}

func validateDraftCreateCommand(commandID string) error {
	spec, ok := draftCommandSpecByID(commandID)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("unknown command id %q", commandID))
	}
	if len(spec.PathParams) == 0 {
		return nil
	}
	return errnorm.Usage(
		"invalid_request",
		fmt.Sprintf(
			"`anx draft create` cannot stage %s because it requires path parameters (%s); use the typed proposal command instead",
			commandID,
			strings.Join(spec.PathParams, ", "),
		),
	)
}

func draftCommandSpecByID(commandID string) (contractsclient.CommandSpec, bool) {
	spec, ok := commandSpecByID(commandID)
	if ok {
		return spec, true
	}
	fallback, ok := draftCommandFallbackSpecs[strings.TrimSpace(commandID)]
	if !ok {
		return contractsclient.CommandSpec{}, false
	}
	return contractsclient.CommandSpec{
		CommandID:  strings.TrimSpace(commandID),
		Method:     "POST",
		InputMode:  "json-body",
		PathParams: append([]string(nil), fallback.pathParams...),
	}, true
}

func validateDraftDocsRevision(body map[string]any) []string {
	out := make([]string, 0)
	if err := validateDocsRevisionBody(body, "docs revise"); err != nil {
		out = append(out, err.Error())
	}
	return out
}

func validateThreadFields(thread map[string]any, createMode bool, path string, out *[]string) {
	stringFields := map[string]bool{
		"title": true,
	}
	stringListFields := map[string]bool{
		"next_actions": true,
	}
	typedRefListFields := map[string]bool{
		"key_artifacts": true,
	}

	for field, raw := range thread {
		full := path + "." + field
		switch field {
		case "status", "type", "priority", "cadence", "next_check_in_at", "current_summary", "tags":
			*out = append(*out, full+" is not supported; lifecycle state is derived from archive/trash timestamps")
			continue
		}
		switch {
		case stringFields[field]:
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if createMode && strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		case stringListFields[field]:
			if _, ok := asStringList(raw); !ok {
				*out = append(*out, full+" must be a list of strings")
			}
		case typedRefListFields[field]:
			values, ok := asStringList(raw)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			validateTypedRefs(values, full, out)
		case field == "provenance":
			provenance, ok := raw.(map[string]any)
			if !ok {
				*out = append(*out, full+" must be an object")
				continue
			}
			validateProvenance(provenance, full, out)
		}
	}
}

func validateDraftTopicCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	topic, ok := requiredObjectField(body, "topic", "topic", &out)
	if !ok {
		return out
	}
	requiredFields := []string{
		"title",
		"summary",
		"owner_refs",
		"document_refs",
		"board_refs",
		"related_refs",
		"provenance",
	}
	for _, field := range requiredFields {
		if _, exists := topic[field]; !exists {
			out = append(out, fmt.Sprintf("topic.%s is required", field))
		}
	}
	validateTopicFields(topic, true, "topic", &out)
	return out
}

func validateDraftTopicPatch(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	validateOptionalRFC3339(body, "if_updated_at", "if_updated_at", &out)
	patch, ok := requiredObjectField(body, "patch", "patch", &out)
	if !ok {
		return out
	}
	if len(patch) == 0 {
		out = append(out, "patch is required")
		return out
	}
	validateTopicFields(patch, false, "patch", &out)
	return out
}

func validateTopicFields(topic map[string]any, createMode bool, path string, out *[]string) {
	stringFields := map[string]bool{
		"title":   true,
		"summary": true,
	}
	for field, raw := range topic {
		full := path + "." + field
		switch field {
		case "status", "state":
			*out = append(*out, full+" is not supported on write; lifecycle state is derived from archive/trash timestamps")
			continue
		case "title", "summary":
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if createMode && strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		case "owner_refs", "document_refs", "board_refs", "related_refs":
			values, ok := asStringList(raw)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			validateTypedRefs(values, full, out)
		case "thread_id":
			if raw == nil {
				continue
			}
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		case "provenance":
			provenance, ok := raw.(map[string]any)
			if !ok {
				*out = append(*out, full+" must be an object")
				continue
			}
			validateProvenance(provenance, full, out)
		default:
			if !stringFields[field] {
				continue
			}
		}
	}
}

func validateDraftCardPatch(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	validateOptionalRFC3339(body, "if_updated_at", "if_updated_at", &out)
	patch, ok := requiredObjectField(body, "patch", "patch", &out)
	if !ok {
		return out
	}
	if len(patch) == 0 {
		out = append(out, "patch is required")
		return out
	}
	validateCardFields(patch, false, "patch", &out)
	return out
}

func validateDraftCardMove(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	requiredStringField(body, "column_key", "column_key", true, &out)
	validateRequiredRFC3339(body, "if_board_updated_at", "if_board_updated_at", &out)
	validateCardFields(body, false, "", &out)
	return out
}

func validateCardFields(card map[string]any, createMode bool, path string, out *[]string) {
	for field, raw := range card {
		full := field
		if path != "" {
			full = path + "." + field
		}
		switch field {
		case "title", "summary", "risk", "resolution":
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if createMode && strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		case "assignee_refs", "resolution_refs", "related_refs":
			values, ok := asStringList(raw)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			validateTypedRefs(values, full, out)
		case "topic_ref", "thread_ref", "document_ref", "before_card_id", "after_card_id":
			if raw == nil {
				continue
			}
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		case "provenance":
			provenance, ok := raw.(map[string]any)
			if !ok {
				*out = append(*out, full+" must be an object")
				continue
			}
			validateProvenance(provenance, full, out)
		case "column_key":
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if createMode && strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		}
	}
}

func validateDraftEventCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	event, ok := requiredObjectField(body, "event", "event", &out)
	if !ok {
		return out
	}
	requiredStringField(event, "type", "event.type", true, &out)
	requiredStringField(event, "summary", "event.summary", false, &out)
	refs, ok := requiredStringListField(event, "refs", "event.refs", 0, &out)
	if ok {
		validateTypedRefs(refs, "event.refs", &out)
	}
	provenance, ok := requiredObjectField(event, "provenance", "event.provenance", &out)
	if ok {
		validateProvenance(provenance, "event.provenance", &out)
	}
	if raw, exists := event["thread_id"]; exists {
		validateNonEmptyString(raw, "event.thread_id", &out)
	}
	if raw, exists := event["payload"]; exists && raw != nil {
		if _, ok := raw.(map[string]any); !ok {
			out = append(out, "event.payload must be an object")
		}
	}
	if err := validateEventsCreateBody(body); err != nil {
		out = append(out, err.Error())
	}
	return out
}

func validateDraftArtifactCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	artifact, ok := requiredObjectField(body, "artifact", "artifact", &out)
	if !ok {
		return out
	}
	kind, hasKind := artifact["kind"].(string)
	if !hasKind || strings.TrimSpace(kind) == "" {
		out = append(out, "artifact.kind is required")
	}
	refs, ok := requiredStringListField(artifact, "refs", "artifact.refs", 0, &out)
	if ok {
		validateTypedRefs(refs, "artifact.refs", &out)
	}
	contentType := ""
	rawContentType, exists := body["content_type"]
	if !exists {
		out = append(out, "content_type is required")
	} else {
		raw, ok := rawContentType.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			out = append(out, "content_type is required")
		} else {
			contentType = strings.TrimSpace(raw)
		}
	}
	content, hasContent := body["content"]
	if !hasContent || content == nil {
		out = append(out, "content is required")
	}

	kind = strings.TrimSpace(kind)
	if !isPacketKind(kind) {
		return out
	}
	if contentType != "structured" {
		out = append(out, "packet artifacts must use content_type=structured")
		return out
	}
	packet, ok := content.(map[string]any)
	if !ok {
		out = append(out, "packet artifacts must provide content as a JSON object")
		return out
	}
	validatePacketForKind(kind, artifact, packet, "content", &out)
	return out
}

// askOutcomeUsage names every outcome the contract accepts for an ask response.
var askOutcomeUsage = "outcome must be one of: " + strings.Join(contractenums.HumanAttentionResponseOutcomeValues, ", ")

func validateDraftInboxRespond(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	requiredStringField(body, "inbox_item_id", "inbox_item_id", true, &out)
	requiredStringField(body, "response_text", "response_text", true, &out)
	requiredStringField(body, "outcome", "outcome", true, &out)
	if outcome := anyString(body["outcome"]); outcome != "" {
		if !contractenums.IsHumanAttentionResponseOutcome(outcome) {
			out = append(out, askOutcomeUsage)
		}
	}
	return out
}

func validateDraftReceiptCreate(body map[string]any) []string {
	return validateDraftPacketCreate(body, "receipt")
}

func validateDraftReviewCreate(body map[string]any) []string {
	return validateDraftPacketCreate(body, "review")
}

func validateDraftPacketCreate(body map[string]any, packetKind string) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	artifact, ok := requiredObjectField(body, "artifact", "artifact", &out)
	if !ok {
		return out
	}
	packet, ok := requiredObjectField(body, "packet", "packet", &out)
	if !ok {
		return out
	}

	if rawKind, hasKind := artifact["kind"]; hasKind {
		text, ok := rawKind.(string)
		if !ok {
			out = append(out, "artifact.kind must be a string")
		} else if strings.TrimSpace(text) != packetKind {
			out = append(out, "artifact.kind must be "+packetKind)
		}
	}
	refs, ok := requiredStringListField(artifact, "refs", "artifact.refs", 0, &out)
	if ok {
		validateTypedRefs(refs, "artifact.refs", &out)
	}
	validatePacketForKind(packetKind, artifact, packet, "packet", &out)
	return out
}

func validatePacketForKind(kind string, artifact map[string]any, packet map[string]any, path string, out *[]string) {
	rules := map[string]map[string]string{
		"receipt": {
			"receipt_id":            "string",
			"subject_ref":           "string",
			"outputs":               "list<typed_ref+>",
			"verification_evidence": "list<typed_ref+>",
			"changes_summary":       "string",
			"known_gaps":            "list<string>",
		},
		"review": {
			"review_id":     "string",
			"subject_ref":   "string",
			"receipt_ref":   "string",
			"outcome":       "string",
			"notes":         "string",
			"evidence_refs": "list<typed_ref>",
		},
	}
	fields, ok := rules[kind]
	if !ok {
		*out = append(*out, fmt.Sprintf("unsupported packet kind %q", kind))
		return
	}

	fieldOrder := []string{}
	switch kind {
	case "receipt":
		fieldOrder = []string{"receipt_id", "subject_ref", "outputs", "verification_evidence", "changes_summary", "known_gaps"}
	case "review":
		fieldOrder = []string{"review_id", "subject_ref", "receipt_ref", "outcome", "notes", "evidence_refs"}
	}
	for _, name := range fieldOrder {
		kindSpec := fields[name]
		full := path + "." + name
		value, exists := packet[name]
		if !exists {
			*out = append(*out, full+" is required")
			continue
		}
		switch kindSpec {
		case "string":
			validateNonEmptyString(value, full, out)
		case "list<string>":
			if _, ok := asStringList(value); !ok {
				*out = append(*out, full+" must be a list of strings")
			}
		case "list<typed_ref>", "list<typed_ref+>":
			values, ok := asStringList(value)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			if kindSpec == "list<typed_ref+>" && len(values) == 0 {
				*out = append(*out, full+" must include at least 1 item")
			}
			validateTypedRefs(values, full, out)
		}
	}

	if kind == "review" {
		if outcomeRaw, exists := packet["outcome"]; exists {
			outcome, ok := outcomeRaw.(string)
			if ok {
				switch strings.TrimSpace(outcome) {
				case "accept", "revise", "escalate":
				default:
					*out = append(*out, "packet.outcome must be one of: accept, revise, escalate")
				}
			}
		}
	}

	idField := map[string]string{
		"receipt": "receipt_id",
		"review":  "review_id",
	}[kind]
	packetID, _ := packet[idField].(string)
	packetID = strings.TrimSpace(packetID)
	if packetID != "" {
		if artifactID, hasArtifactID := artifact["id"].(string); hasArtifactID {
			artifactID = strings.TrimSpace(artifactID)
			if artifactID != "" && artifactID != packetID {
				*out = append(*out, fmt.Sprintf("packet.%s must equal artifact.id", idField))
			}
		}
	}

	artifactRefs, refsOK := asStringList(artifact["refs"])
	if !refsOK {
		return
	}
	subjectRef, _ := packet["subject_ref"].(string)
	subjectRef = strings.TrimSpace(subjectRef)
	if subjectRef != "" && !containsRef(artifactRefs, subjectRef) {
		*out = append(*out, fmt.Sprintf("artifact.refs must include %q", subjectRef))
	}
	if kind == "review" {
		receiptRef, _ := packet["receipt_ref"].(string)
		receiptRef = strings.TrimSpace(receiptRef)
		if receiptRef != "" && !containsRef(artifactRefs, receiptRef) {
			*out = append(*out, fmt.Sprintf("artifact.refs must include %q", receiptRef))
		}
	}
}

func validateDraftDerivedRebuild(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	return out
}

func isPacketKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "receipt", "review":
		return true
	default:
		return false
	}
}
