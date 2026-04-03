package app

import (
	"strings"

	"organization-autorunner-cli/internal/config"
)

type conceptsPrimitive struct {
	Name        string
	UseWhen     string
	NotFor      string
	Examples    []string
	RelatedRead []string
}

type namedDescription struct {
	Name        string
	Description string
}

var conceptsGuidePrimitives = []conceptsPrimitive{
	{
		Name:        "threads",
		UseWhen:     "You need a durable work object with ownership, status, cadence, summary, and follow-up over time.",
		NotFor:      "Append-only facts or long-form narrative documents.",
		Examples:    []string{"initiatives", "incidents", "cases", "deliverables"},
		RelatedRead: []string{"oar threads list", "oar threads get", "oar threads review"},
	},
	{
		Name:        "events",
		UseWhen:     "You need immutable facts, observations, decisions, or updates in an auditable sequence.",
		NotFor:      "Replacing the current durable state of a work object.",
		Examples:    []string{"decision_needed", "decision_made", "message_posted", "exception_raised"},
		RelatedRead: []string{"oar events list", "oar events explain", "oar threads timeline"},
	},
	{
		Name:        "docs",
		UseWhen:     "You need long-lived narrative knowledge that should be revised, read, and referenced as a document.",
		NotFor:      "Ephemeral chat-like updates or board membership.",
		Examples:    []string{"plans", "notes", "decision records", "runbooks"},
		RelatedRead: []string{"oar docs list", "oar docs get", "oar docs content"},
	},
	{
		Name:        "boards",
		UseWhen:     "You need a coordination view across multiple work items with explicit workflow columns and ordering.",
		NotFor:      "Being the source of truth for the work itself.",
		Examples:    []string{"triage board", "release board", "initiative tracking board"},
		RelatedRead: []string{"oar boards list", "oar boards workspace", "oar boards cards list"},
	},
	{
		Name:        "inbox",
		UseWhen:     "You need the derived queue of what currently needs attention from the active actor's perspective.",
		NotFor:      "Durable automation contracts or historical truth.",
		Examples:    []string{"pending decisions", "exceptions", "commitment risk"},
		RelatedRead: []string{"oar inbox list", "oar inbox get", "oar inbox ack"},
	},
	{
		Name:        "draft",
		UseWhen:     "You want to stage a mutation locally, inspect it, then apply it explicitly.",
		NotFor:      "Read paths or append-only event authoring.",
		Examples:    []string{"reviewable thread patches", "reviewable doc updates"},
		RelatedRead: []string{"oar draft create", "oar draft list", "oar draft commit"},
	},
}

var inboxCategoryReference = []namedDescription{
	{Name: "decision_needed", Description: "A human must choose among multiple viable paths."},
	{Name: "intervention_needed", Description: "The next step is clear, but a human must act because the agent cannot execute it."},
	{Name: "exception", Description: "Investigate an exception, risk, or broken expectation on the thread."},
	{Name: "commitment_risk", Description: "A commitment is at risk or overdue and needs follow-up."},
}

func inboxCategoryReferenceMap() map[string]string {
	out := make(map[string]string, len(inboxCategoryReference))
	for _, entry := range inboxCategoryReference {
		out[entry.Name] = entry.Description
	}
	return out
}

func inboxCategoryDescription(name string) string {
	name = strings.TrimSpace(name)
	for _, entry := range inboxCategoryReference {
		if entry.Name == name {
			return entry.Description
		}
	}
	return ""
}

func conceptsGuideData() map[string]any {
	primitives := make([]map[string]any, 0, len(conceptsGuidePrimitives))
	for _, primitive := range conceptsGuidePrimitives {
		primitives = append(primitives, map[string]any{
			"name":         primitive.Name,
			"use_when":     primitive.UseWhen,
			"not_for":      primitive.NotFor,
			"examples":     append([]string(nil), primitive.Examples...),
			"related_read": append([]string(nil), primitive.RelatedRead...),
		})
	}
	return map[string]any{
		"guide_topic":       "concepts",
		"summary":           "Quick guide to the core OAR primitives and when to use each.",
		"primitives":        primitives,
		"selection_rules":   conceptsSelectionRules(),
		"inbox_categories":  inboxCategoryReferenceMap(),
		"recommended_reads": []string{"oar help", "oar meta doc concepts", "oar meta doc agent-guide"},
	}
}

func conceptsSelectionRules() []string {
	return []string{
		"Use events for immutable facts.",
		"Use threads for durable work state and coordination.",
		"Use docs for narrative knowledge that should be revised over time.",
		"Use boards for cross-object workflow views, not source-of-truth content.",
		"Use inbox for current attention signals from the active CLI identity's perspective.",
		"Use draft when you want a local review checkpoint before a write.",
	}
}

func conceptsGuideText() string {
	var b strings.Builder
	b.WriteString("OAR concepts guide\n\n")
	b.WriteString("Use this command when you need to decide which primitive fits the task before you start issuing writes.\n\n")
	b.WriteString("Selection rules:\n")
	for _, rule := range conceptsSelectionRules() {
		b.WriteString("- ")
		b.WriteString(rule)
		b.WriteString("\n")
	}
	for _, primitive := range conceptsGuidePrimitives {
		b.WriteString("\n")
		b.WriteString(primitive.Name)
		b.WriteString("\n")
		b.WriteString("- Use when: ")
		b.WriteString(primitive.UseWhen)
		b.WriteString("\n")
		b.WriteString("- Not for: ")
		b.WriteString(primitive.NotFor)
		b.WriteString("\n")
		if len(primitive.Examples) > 0 {
			b.WriteString("- Examples: ")
			b.WriteString(strings.Join(primitive.Examples, ", "))
			b.WriteString("\n")
		}
		if len(primitive.RelatedRead) > 0 {
			b.WriteString("- Read next: ")
			b.WriteString(strings.Join(primitive.RelatedRead, " ; "))
			b.WriteString("\n")
		}
	}
	b.WriteString("\nInbox categories:\n")
	for _, entry := range inboxCategoryReference {
		b.WriteString("- `")
		b.WriteString(entry.Name)
		b.WriteString("`: ")
		b.WriteString(entry.Description)
		b.WriteString("\n")
	}
	b.WriteString("\nFor the fuller operating model, read `oar meta doc agent-guide`.\n")
	return strings.TrimSpace(b.String())
}

func viewingAsData(cfg config.Resolved) map[string]any {
	out := map[string]any{}
	if profile := strings.TrimSpace(cfg.Agent); profile != "" {
		out["profile"] = profile
	}
	if username := strings.TrimSpace(cfg.Username); username != "" {
		out["username"] = username
	}
	if actorID := strings.TrimSpace(cfg.ActorID); actorID != "" {
		out["actor_id"] = actorID
	}
	return out
}

func formatViewingAsSummary(raw any) string {
	viewing, _ := raw.(map[string]any)
	if viewing == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if profile := strings.TrimSpace(anyString(viewing["profile"])); profile != "" {
		parts = append(parts, "profile="+profile)
	}
	if username := strings.TrimSpace(anyString(viewing["username"])); username != "" {
		parts = append(parts, "username="+username)
	}
	if actorID := strings.TrimSpace(anyString(viewing["actor_id"])); actorID != "" {
		parts = append(parts, "actor_id="+actorID)
	}
	return strings.Join(parts, " :: ")
}
