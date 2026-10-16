package server

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

func activityImpactRank(impact string) int {
	switch impact {
	case "high":
		return 2
	case "normal":
		return 1
	}
	return 0
}

// Prefer the actual changed item over its containing board or related evidence.
func activitySubjectRef(event map[string]any) string {
	p, _ := event["payload"].(map[string]any)
	if ref := anyString(p["subject_ref"]); ref != "" {
		return ref
	}
	for _, kind := range []string{"card", "document", "doc", "topic", "board"} {
		if id := anyString(p[kind+"_id"]); id != "" {
			return kind + ":" + id
		}
		for _, ref := range stringSliceAny(event["refs"]) {
			if strings.HasPrefix(ref, kind+":") {
				return ref
			}
		}
	}
	if id := eventThreadID(event); id != "" {
		return "thread:" + id
	}
	return ""
}

func (reader *reportReader) prepareActivity() error {
	if reader.activitySubjects != nil {
		return nil
	}
	reader.activitySubjects = map[string]map[string]any{}
	// Ask titles live on immutable request events. Responses can reuse only
	// requests already admitted in this reader's bounded, scoped event page.
	for _, event := range reader.events {
		if event["type"] != "human_attention_requested" {
			continue
		}
		p := activityMap(event["payload"])
		title := anyString(p["title"])
		id := anyString(event["id"])
		if id == "" {
			id = strings.TrimPrefix(anyString(event["ref"]), "event:")
		}
		if title == "" || id == "" {
			continue
		}
		subject := map[string]any{"ref": "inbox:" + id, "title": title, "kind": "ask"}
		for _, key := range []string{anyString(event["ref"]), "event:" + id, "inbox:" + id} {
			reader.activitySubjects[key] = subject
		}
	}

	refs := []string{}
	seen := map[string]bool{}
	for _, event := range reader.events {
		ref := activitySubjectRef(event)
		if ref != "" && !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	store, ok := reader.opts.primitiveStore.(interface {
		ReportActivitySubjects(context.Context, []string, func(string, string) bool) ([]primitives.RefPreview, error)
	})
	if !ok { // Alternative stores reuse titles already authorized by activeEvent.
		return nil
	}
	for start := 0; start < len(refs); start += maxReportCandidates {
		batch := refs[start:min(start+maxReportCandidates, len(refs))]
		visible := planVisibility(reader.r, reader.opts)
		if _, scoped := resourceaccess.PolicyFrom(reader.r.Context()); !scoped {
			visible = nil
		}
		facts, err := store.ReportActivitySubjects(reader.r.Context(), batch, visible)
		if err != nil {
			return err
		}
		for i, fact := range facts {
			if !fact.Resolvable {
				continue
			}
			kind := fact.Kind
			switch kind {
			case "document":
				kind = "doc"
			case "card", "initiative", "board", "topic":
			default:
				continue
			}
			reader.activitySubjects[batch[i]] = map[string]any{"ref": fact.Ref, "title": fact.Title, "kind": kind}
		}
	}
	return nil
}

func (reader *reportReader) prepareActivityActors(items []map[string]any) error {
	if reader.actorLabels == nil {
		reader.actorLabels = map[string]string{}
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		id := anyString(item["actor_id"])
		if _, ok := reader.actorLabels[id]; !ok && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}

	if reader.opts.actorRegistry != nil {
		for start := 0; start < len(ids); start += maxReportCandidates {
			names, err := reader.opts.actorRegistry.DisplayNames(reader.r.Context(), ids[start:min(start+maxReportCandidates, len(ids))])
			if err != nil {
				return err
			}
			for id, name := range names {
				if name != "" {
					reader.actorLabels[id] = name
				}
			}
		}
	}

	for _, id := range ids {
		if _, ok := reader.actorLabels[id]; !ok {
			reader.actorLabels[id] = id
		}
	}
	return nil
}
func (reader *reportReader) activityActorLabel(id string) string {
	if label := reader.actorLabels[id]; label != "" {
		return label
	}
	return id
}

func (reader *reportReader) activityItem(event map[string]any) map[string]any {
	kind := anyString(event["type"])
	p, _ := event["payload"].(map[string]any)
	if kind == "message_posted" {
		next, _ := p["next"].(map[string]any)
		if anyString(next["at"]) == "" {
			return nil
		}
	}
	ref := activitySubjectRef(event)
	var subject any
	if known := reader.activitySubjects[ref]; known != nil {
		copy := map[string]any{}
		for k, v := range known {
			copy[k] = v
		}
		subject = copy
	} else if reader.labels[ref] != "" && reader.visibility[ref] && !reader.hidden[ref] {
		k, _, _ := strings.Cut(ref, ":")
		if k == "document" {
			k = "doc"
		}
		if k != "thread" {
			subject = map[string]any{"ref": ref, "title": reader.labels[ref], "kind": k}
		}
	}
	// A missing subject never borrows a title from an event's historical payload.
	if s, ok := subject.(map[string]any); ok && s["kind"] == "card" && (p["plan"] != nil || containsActivityField(p, "plan")) {
		s["kind"] = "initiative"
	}

	if subject == nil && kind == "human_attention_requested" {
		subject = reader.activitySubjects[anyString(event["ref"])]
	}
	if subject == nil && kind == "human_attention_responded" {
		for _, key := range []string{anyString(p["request_event_ref"]), "inbox:" + anyString(p["inbox_item_id"])} {
			if known := reader.activitySubjects[key]; known != nil {
				subject = known
				break
			}
		}
	}

	verb, detail, impact := classifyReportActivity(kind, p)
	if (kind == "card_created" || containsActivityField(p, "work")) && activityMap(subject)["kind"] == "initiative" {
		impact = "high"
	}
	return map[string]any{"ref": event["ref"], "type": kind, "summary": event["summary"], "actor_id": anyString(event["actor_id"]), "actor_label": reader.activityActorLabel(anyString(event["actor_id"])), "ts": event["ts"], "count": 1, "_coalesce_ref": ref, "subject": subject, "verb": verb, "detail": activityPlainText(detail, 140), "impact": impact}
}
func containsActivityField(p map[string]any, field string) bool {
	for _, f := range stringSliceAny(p["changed_fields"]) {
		if f == field {
			return true
		}
	}
	return false
}
func activityMap(value any) map[string]any              { p, _ := value.(map[string]any); return p }
func activityPhase(p map[string]any, key string) string { return strings.ToLower(anyString(p[key])) }
func activityPhaseLabel(phase string) string {
	phase = strings.ReplaceAll(phase, "_", " ")
	if phase == "" {
		return "Unknown"
	}
	r := []rune(phase)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Classification uses event facts, never summary string matching or current
// state (which may have changed again since the event).
func classifyReportActivity(kind string, p map[string]any) (verb, detail, impact string) {
	verb, impact = "edited", "low"
	switch kind {
	case "human_attention_requested":
		return "asked", "", "high"
	case "human_attention_responded":
		return "answered", activityPlainText(anyString(p["response_text"]), 140), "high"
	case "card_created":
		return "created", "", "normal"
	case "card_resolved", "card_closed":
		if resolution := activityPhase(p, "resolution"); resolution == "cancelled" || resolution == "canceled" {
			return "cancelled", "", "high"
		}
		return "completed", "", "high"
	case "document_created", "topic_created":
		return "created", "", "normal"
	case "document_updated", "topic_updated":
		return "edited", "", "normal"
	}
	if kind == "board_updated" {
		return "edited", "Board metadata changed", "low"
	}
	if kind == "card_updated" && containsActivityField(p, "work") {
		return "created", "", "normal"
	}
	patch := activityMap(p["patch"])
	before, after := activityPhase(p, "from_column_key"), activityPhase(p, "column_key")
	if after == "" {
		after = activityPhase(p, "phase")
		before = activityPhase(p, "previous_phase")
	}
	if _, present := p["resolution"]; present {
		after = activityPhase(p, "resolution")
		before = activityPhase(p, "previous_resolution")
	}
	if before == "" {
		before = activityPhase(p, "previous_column_key")
	}
	if containsActivityField(p, "resolution") && after == "" && (before == "done" || before == "cancelled" || before == "completed") {
		return "reopened", "Resolution cleared", "high"
	}
	if after != "" && (before != after) && (kind == "card_moved" || containsActivityField(p, "resolution") || containsActivityField(p, "phase") || containsActivityField(p, "column_key")) {
		verb = "moved"
		switch {
		case after == "done" || after == "completed":
			verb = "completed"
		case after == "cancelled" || after == "canceled":
			verb = "cancelled"
		case before == "done" || before == "completed" || before == "cancelled" || before == "canceled":
			verb = "reopened"
		}
		return verb, activityPhaseLabel(before) + " → " + activityPhaseLabel(after), "high"
	}
	if kind == "card_moved" {
		return "moved", "Reordered", "low"
	} // Same-column ranking is cosmetic.
	if containsActivityField(p, "plan") || p["plan"] != nil || patch["plan"] != nil {
		verb, impact = "updated the plan of", "normal"
		var old, current plans.Plan
		raw, _ := json.Marshal(p["before_plan"])
		_ = json.Unmarshal(raw, &old)
		value := p["plan"]
		if value == nil {
			value = patch["plan"]
		}
		raw, _ = json.Marshal(value)
		_ = json.Unmarshal(raw, &current)
		previous := map[string]plans.Step{}
		for _, step := range old.Steps {
			previous[step.ID] = step
		}
		done, newDone := 0, 0
		promise := ""
		for _, step := range current.Steps {
			was := previous[step.ID]
			if step.Status == "done" {
				done++
				if was.Status != "done" {
					newDone++
				}
			}
			if step.Due != was.Due {
				promise = promiseActivityDetail("due_at", step.Due)
			}
		}
		detail = fmt.Sprintf("%d/%d steps done", done, len(current.Steps))
		if promise != "" {
			detail, impact = promise, "high"
		}
		if newDone > 0 {
			unit := "steps"
			if newDone == 1 {
				unit = "step"
			}
			detail, impact = fmt.Sprintf("%d %s done · %d/%d", newDone, unit, done, len(current.Steps)), "high"
		}
		return
	}
	next := activityMap(p["next"])
	if at := anyString(next["at"]); at != "" {
		return "set a promise for", "promise set for " + at, "high"
	}
	for _, field := range []string{"due_at", "wake_condition"} {
		if value, present := patch[field]; present {
			return "changed the promise of", promiseActivityDetail(field, value), "high"
		}
		if containsActivityField(p, field) {
			return "changed the promise of", promiseActivityDetail(field, p[field]), "high"
		}
	}
	for _, field := range []string{"title", "priority", "assignee", "assignee_refs", "risk", "summary", "body", "next_actor", "next_action"} {
		if containsActivityField(p, field) || patch[field] != nil {
			return "edited", strings.ReplaceAll(field, "_", " ") + " changed", "normal"
		}
	}
	return
}
func promiseActivityDetail(field string, value any) string {
	if field == "wake_condition" {
		return "Promise condition changed"
	}
	if anyString(value) == "" {
		return "Promise date cleared"
	}
	return "promise moved to " + anyString(value)
}

// The newest event anchors a fixed 30-minute window; a chain cannot absorb an
// entire day. Events are already ordered newest first. An unresolved card keeps
// its original target identity and never falls back to a board; events without
// any target identity do not coalesce.
func coalesceReportActivity(items []map[string]any) []map[string]any {
	out := []map[string]any{}
	bursts := map[string]int{}
	for _, item := range items {
		subject := activityMap(item["subject"])
		ref := anyString(subject["ref"])
		if ref == "" {
			ref = anyString(item["_coalesce_ref"])
		}
		delete(item, "_coalesce_ref")
		if ref == "" {
			out = append(out, item)
			continue
		}
		key := anyString(item["actor_id"]) + "/" + ref
		at, atErr := time.Parse(time.RFC3339Nano, anyString(item["ts"]))
		if index, ok := bursts[key]; ok {
			existing := out[index]
			newest, newestErr := time.Parse(time.RFC3339Nano, anyString(existing["ts"]))
			if atErr == nil && newestErr == nil && newest.Sub(at) >= 0 && newest.Sub(at) <= 30*time.Minute {
				existing["count"] = existing["count"].(int) + item["count"].(int)
				oldRank, newRank := activityImpactRank(anyString(existing["impact"])), activityImpactRank(anyString(item["impact"]))
				if newRank > oldRank {
					for _, field := range []string{"impact", "verb", "detail", "subject", "summary"} {
						existing[field] = item[field]
					}
				}
				// Equal ranks retain the newest representative, even when its
				// detail is empty. Older details must not overwrite its action.
				continue
			}
		}
		bursts[key] = len(out)
		out = append(out, item)
	}
	return out
}

var activityLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
var activityHTML = regexp.MustCompile(`<[^>]*>`)

func activityPlainText(value string, limit int) string {
	// Bound excerpt processing independently of the stored response size.
	if len(value) > 8192 {
		value = value[:8192]
	}
	value = activityLink.ReplaceAllString(value, "$1")
	value = activityHTML.ReplaceAllString(value, "")
	value = strings.Map(func(r rune) rune {
		if strings.ContainsRune("*_`#[]", r) {
			return -1
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	r := []rune(value)
	if len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return value
}
