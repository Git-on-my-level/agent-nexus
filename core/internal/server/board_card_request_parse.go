package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"organization-autorunner-core/internal/primitives"
)

// addBoardCardMerged is the normalized board card create request after merging the
// contract `card` envelope with legacy top-level fields.
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
	Priority         *string
	Status           string
	PinnedDocumentID *string
	Resolution       *string
	ResolutionRefs   []string
	Refs             []string
	Risk             *string
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

	pick := func(key string) any {
		if cardObj != nil {
			if v, ok := cardObj[key]; ok && v != nil {
				if s := strings.TrimSpace(anyString(v)); s != "" || key == "summary" || key == "title" {
					// allow explicit empty strings for title/summary where provided
					return v
				}
			}
		}
		if v, ok := raw[key]; ok {
			return v
		}
		return nil
	}

	m.CardID = strings.TrimSpace(anyString(pick("card_id")))
	if m.CardID == "" {
		m.CardID = strings.TrimSpace(anyString(pick("id")))
	}

	m.Title = strings.TrimSpace(anyString(pick("title")))
	m.Body = strings.TrimSpace(anyString(pick("summary")))
	if m.Body == "" {
		m.Body = strings.TrimSpace(anyString(pick("body")))
	}

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
	} else {
		m.Assignee = normalizeOptionalRequestStringPointer(assigneeStringPtr(pick("assignee")))
	}

	m.Priority = normalizeOptionalRequestStringPointer(assigneeStringPtr(pick("priority")))
	m.Status = strings.TrimSpace(anyString(pick("status")))

	if v := pick("pinned_document_id"); v != nil {
		m.PinnedDocumentID = normalizeOptionalRequestStringPointer(assigneeStringPtr(v))
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
	if rawLR := pick("refs"); rawLR != nil {
		lr, err := extractStringSlice(rawLR)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "refs must be a list of strings")
			return m, false
		}
		refs = append(refs, lr...)
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
		m.Status,
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
		Priority:         m.Priority,
		Status:           createStatus,
		ThreadID:         m.ThreadID,
		ColumnKey:        m.ColumnKey,
		BeforeCardID:     m.BeforeCardID,
		AfterCardID:      m.AfterCardID,
		BeforeThreadID:   m.BeforeThreadID,
		AfterThreadID:    m.AfterThreadID,
		PinnedDocumentID: m.PinnedDocumentID,
		Resolution:       normalizeOptionalRequestStringPointer(m.Resolution),
		ResolutionRefs:   m.ResolutionRefs,
		Refs:             m.Refs,
		Risk:             m.Risk,
		IfBoardUpdatedAt: m.IfBoardUpdatedAt,
	}
}
