package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"organization-autorunner-core/internal/primitives"
)

// addBoardCardMerged is the normalized board card create request after merging the
// contract `card` envelope with the canonical top-level transport fields.
type addBoardCardMerged struct {
	ActorID          string
	RequestKey       string
	CardID           string
	IfBoardUpdatedAt *string
	Title            string
	Body             string
	ParentThread     string
	ThreadID         string
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
	BeforeThreadID   string
	AfterThreadID    string
	DueAt            *string
	DefinitionOfDone []string
	Assignee         *string
	PinnedDocumentID *string
	Resolution       *string
	ResolutionRefs   []string
	Refs             []string
	Risk             *string
}

func mixedBoardCardAliasError(canonicalField string, legacyFields ...string) string {
	if len(legacyFields) == 0 {
		return canonicalField + " must not be combined with legacy aliases"
	}
	return canonicalField + " must not be combined with legacy aliases " + strings.Join(legacyFields, ", ")
}

func firstPresentBoardCardValue(raw map[string]any, cardObj map[string]any, key string) any {
	if cardObj != nil {
		if v, ok := cardObj[key]; ok && v != nil {
			if s, ok := v.(string); !ok || strings.TrimSpace(s) != "" || key == "summary" || key == "title" {
				return v
			}
		}
	}
	if v, ok := raw[key]; ok {
		return v
	}
	return nil
}

func hasBoardCardField(raw map[string]any, cardObj map[string]any, key string) bool {
	if cardObj != nil {
		if _, ok := cardObj[key]; ok {
			return true
		}
	}
	_, ok := raw[key]
	return ok
}

func parseAddBoardCardJSON(w http.ResponseWriter, raw map[string]any) (addBoardCardMerged, bool) {
	var m addBoardCardMerged
	if raw == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "body is required")
		return m, false
	}

	m.ActorID = strings.TrimSpace(anyString(raw["actor_id"]))
	m.RequestKey = strings.TrimSpace(anyString(raw["request_key"]))

	if rawIf, ok := raw["if_board_updated_at"]; ok && rawIf != nil {
		s := strings.TrimSpace(anyString(rawIf))
		if s != "" {
			m.IfBoardUpdatedAt = &s
		}
	}

	var cardObj map[string]any
	if c, ok := raw["card"].(map[string]any); ok && c != nil {
		cardObj = c
	}

	pick := func(key string) any { return firstPresentBoardCardValue(raw, cardObj, key) }

	if hasBoardCardField(raw, cardObj, "summary") && (hasBoardCardField(raw, cardObj, "body") || hasBoardCardField(raw, cardObj, "body_markdown")) {
		writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("summary", "body", "body_markdown"))
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "body") {
		writeError(w, http.StatusBadRequest, "invalid_request", "body is not supported; use summary")
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "body_markdown") {
		writeError(w, http.StatusBadRequest, "invalid_request", "body_markdown is not supported; use summary")
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "assignee") {
		writeError(w, http.StatusBadRequest, "invalid_request", "assignee is not supported; use assignee_refs")
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "document_ref") && hasBoardCardField(raw, cardObj, "pinned_document_id") {
		writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("document_ref", "pinned_document_id"))
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "pinned_document_id") {
		writeError(w, http.StatusBadRequest, "invalid_request", "pinned_document_id is not supported; use document_ref")
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "related_refs") && hasBoardCardField(raw, cardObj, "refs") {
		writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("related_refs", "refs"))
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "refs") {
		writeError(w, http.StatusBadRequest, "invalid_request", "refs is not supported; use related_refs and topic_ref")
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "priority") {
		writeError(w, http.StatusBadRequest, "invalid_request", "priority is not supported in the canonical card model")
		return m, false
	}
	if hasBoardCardField(raw, cardObj, "status") {
		writeError(w, http.StatusBadRequest, "invalid_request", "status is not supported on card create; column_key and resolution define lifecycle")
		return m, false
	}

	m.CardID = strings.TrimSpace(anyString(pick("card_id")))
	if m.CardID == "" {
		m.CardID = strings.TrimSpace(anyString(pick("id")))
	}

	m.Title = strings.TrimSpace(anyString(pick("title")))
	m.Body = strings.TrimSpace(anyString(pick("summary")))

	m.ParentThread = strings.TrimSpace(anyString(pick("parent_thread")))
	m.ThreadID = strings.TrimSpace(anyString(pick("thread_id")))
	m.ColumnKey = strings.TrimSpace(anyString(pick("column_key")))
	m.BeforeCardID = strings.TrimSpace(anyString(pick("before_card_id")))
	m.AfterCardID = strings.TrimSpace(anyString(pick("after_card_id")))
	m.BeforeThreadID = strings.TrimSpace(anyString(pick("before_thread_id")))
	m.AfterThreadID = strings.TrimSpace(anyString(pick("after_thread_id")))

	if v := pick("due_at"); v != nil {
		s := strings.TrimSpace(anyString(v))
		if s != "" {
			m.DueAt = &s
		}
	}

	if rawDod := pick("definition_of_done"); rawDod != nil {
		dod, err := extractStringSlice(rawDod)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "definition_of_done must be a list of strings")
			return m, false
		}
		m.DefinitionOfDone = uniqueSortedStrings(dod)
	}

	if rawAR := pick("assignee_refs"); rawAR != nil {
		ar, err := extractStringSlice(rawAR)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "assignee_refs must be a list of strings")
			return m, false
		}
		if len(ar) > 0 {
			m.Assignee = assigneeStorageStringFromRefs(uniqueSortedStrings(ar))
		} else {
			empty := ""
			m.Assignee = &empty
		}
	}
	if dr := strings.TrimSpace(anyString(pick("document_ref"))); dr != "" {
		pid, err := pinnedDocumentIDFromTypedRef(dr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return m, false
		}
		if pid != nil && strings.TrimSpace(*pid) != "" {
			m.PinnedDocumentID = pid
		}
	}

	if rawRfs := pick("resolution_refs"); rawRfs != nil {
		rfs, err := extractStringSlice(rawRfs)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "resolution_refs must be a list of strings")
			return m, false
		}
		m.ResolutionRefs = uniqueSortedStrings(rfs)
	}

	if res := pick("resolution"); res != nil {
		s := strings.TrimSpace(anyString(res))
		m.Resolution = &s
	}

	var refs []string
	if rawRR := pick("related_refs"); rawRR != nil {
		rr, err := extractStringSlice(rawRR)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "related_refs must be a list of strings")
			return m, false
		}
		refs = append(refs, rr...)
	}
	if tr := strings.TrimSpace(anyString(pick("topic_ref"))); tr != "" {
		refs = append(refs, tr)
	}
	m.Refs = uniqueSortedStrings(refs)

	if risk := strings.TrimSpace(anyString(pick("risk"))); risk != "" {
		switch risk {
		case "low", "medium", "high", "critical":
			r := risk
			m.Risk = &r
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "risk must be one of: low, medium, high, critical")
			return m, false
		}
	}

	if m.Title == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "title is required")
		return m, false
	}

	if m.IfBoardUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, m.IfBoardUpdatedAt, "if_board_updated_at")
		if !ok {
			return m, false
		}
		m.IfBoardUpdatedAt = &normalized
	}

	if err := validateBoardCardCreateRequest(
		m.CardID,
		m.ParentThread,
		m.ThreadID,
		m.ColumnKey,
		m.BeforeCardID,
		m.AfterCardID,
		m.BeforeThreadID,
		m.AfterThreadID,
		m.PinnedDocumentID,
	); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return m, false
	}
	if err := validateBoardCardCreateResolutionInput(m.Resolution, m.ResolutionRefs, m.ColumnKey); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return m, false
	}

	return m, true
}

func validateBoardCardCreateResolutionInput(resolution *string, resolutionRefs []string, columnKey string) error {
	if resolution != nil {
		normalizedResolution := strings.TrimSpace(*resolution)
		if normalizedResolution == "completed" || normalizedResolution == "superseded" {
			normalizedResolution = "done"
		}
		if normalizedResolution == "unresolved" {
			normalizedResolution = ""
		}
		if normalizedResolution != "" {
			if err := validateCardResolution(normalizedResolution, false); err != nil {
				return err
			}
			if strings.TrimSpace(columnKey) != "done" {
				return errors.New("resolution requires column_key done")
			}
			if len(resolutionRefs) == 0 {
				return errors.New("resolution_refs are required when resolution is set")
			}
			if err := validateMoveCardResolutionRefs(normalizedResolution, resolutionRefs); err != nil {
				return err
			}
		}
	}
	if resolution == nil && len(resolutionRefs) > 0 {
		return errors.New("resolution_refs require resolution")
	}
	return nil
}

func rejectMixedBoardCardPatchAliases(w http.ResponseWriter, patch map[string]any) bool {
	if patch == nil {
		return false
	}
	if _, hasSummary := patch["summary"]; hasSummary {
		if _, hasBody := patch["body"]; hasBody {
			writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("patch.summary", "patch.body"))
			return true
		}
		if _, hasBodyMarkdown := patch["body_markdown"]; hasBodyMarkdown {
			writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("patch.summary", "patch.body_markdown"))
			return true
		}
	}
	if _, hasBody := patch["body"]; hasBody {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.body is not supported; use patch.summary")
		return true
	}
	if _, hasAssignee := patch["assignee"]; hasAssignee {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.assignee is not supported; use patch.assignee_refs")
		return true
	}
	if _, hasDocumentRef := patch["document_ref"]; hasDocumentRef {
		if _, hasPinnedDocumentID := patch["pinned_document_id"]; hasPinnedDocumentID {
			writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("patch.document_ref", "patch.pinned_document_id"))
			return true
		}
	}
	if _, hasPinnedDocumentID := patch["pinned_document_id"]; hasPinnedDocumentID {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.pinned_document_id is not supported; use patch.document_ref")
		return true
	}
	if _, hasRelatedRefs := patch["related_refs"]; hasRelatedRefs {
		if _, hasRefs := patch["refs"]; hasRefs {
			writeError(w, http.StatusBadRequest, "invalid_request", mixedBoardCardAliasError("patch.related_refs", "patch.refs"))
			return true
		}
	}
	if _, hasRefs := patch["refs"]; hasRefs {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.refs is not supported; use patch.related_refs and patch.topic_ref")
		return true
	}
	if _, hasPriority := patch["priority"]; hasPriority {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.priority is not supported in the canonical card model")
		return true
	}
	if _, hasStatus := patch["status"]; hasStatus {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.status is not supported; use the move endpoint and patch.resolution")
		return true
	}
	if _, hasBodyMarkdown := patch["body_markdown"]; hasBodyMarkdown {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch.body_markdown is not supported; use patch.summary")
		return true
	}
	return false
}

func assigneeStringPtr(v any) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(anyString(v))
	if s == "" {
		return nil
	}
	return &s
}

// flattenLegacyMoveCardEnvelope promotes nested {"move":{...}} to the root when the root
// does not already set column_key. Canonical shape is flat (refactor spec §8.1); a nested
// move wrapper was historically described in OpenAPI.
func flattenLegacyMoveCardEnvelope(raw map[string]any) {
	if raw == nil {
		return
	}
	moveObj, ok := raw["move"].(map[string]any)
	if !ok || moveObj == nil {
		return
	}
	if strings.TrimSpace(anyString(raw["column_key"])) != "" {
		delete(raw, "move")
		return
	}
	for k, v := range moveObj {
		if _, exists := raw[k]; !exists {
			raw[k] = v
		}
	}
	delete(raw, "move")
}

// decodeMoveCardHTTPPayload decodes JSON then applies legacy move envelope flattening.
func decodeMoveCardHTTPPayload(w http.ResponseWriter, r *http.Request, dst any) bool {
	var raw map[string]any
	if !decodeJSONBody(w, r, &raw) {
		return false
	}
	flattenLegacyMoveCardEnvelope(raw)
	payload, err := json.Marshal(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	if err := json.Unmarshal(payload, dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	return true
}

func addBoardCardStoreInput(m addBoardCardMerged, createStatus string) primitives.AddBoardCardInput {
	return primitives.AddBoardCardInput{
		CardID:           m.CardID,
		Title:            m.Title,
		Body:             m.Body,
		ParentThreadID:   m.ParentThread,
		DueAt:            m.DueAt,
		DefinitionOfDone: m.DefinitionOfDone,
		Assignee:         m.Assignee,
		Status:           createStatus,
		ColumnKey:        m.ColumnKey,
		BeforeCardID:     m.BeforeCardID,
		AfterCardID:      m.AfterCardID,
		PinnedDocumentID: m.PinnedDocumentID,
		Resolution:       normalizeOptionalRequestStringPointer(m.Resolution),
		ResolutionRefs:   m.ResolutionRefs,
		Refs:             m.Refs,
		Risk:             m.Risk,
		IfBoardUpdatedAt: m.IfBoardUpdatedAt,
	}
}
