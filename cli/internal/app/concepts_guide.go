package app

import (
	"strings"

	"agent-nexus-cli/internal/config"
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
		Name:        "topics",
		UseWhen:     "You need a topic-centered discussion and coordination surface for a project, incident, decision, recurring process, or durable work subject.",
		NotFor:      "Tracking active work status across columns or storing long-term reference material.",
		Examples:    []string{"project discussion", "incident coordination", "decision thread", "recurring process"},
		RelatedRead: []string{"anx topics list", "anx topics get", "anx topics workspace"},
	},
	{
		Name:        "boards",
		UseWhen:     "You need active work tracking with workflow columns, cards, ownership, ordering, and movement.",
		NotFor:      "Free-form discussion or durable institutional knowledge.",
		Examples:    []string{"triage board", "release board", "initiative tracking board"},
		RelatedRead: []string{"anx boards list", "anx boards workspace", "anx boards cards list"},
	},
	{
		Name:        "docs",
		UseWhen:     "You need long-term relevant context or institutional knowledge that should be written, revised, read, and referenced as a document.",
		NotFor:      "Ephemeral discussion or active-work status movement.",
		Examples:    []string{"specs", "runbooks", "briefs", "decision records"},
		RelatedRead: []string{"anx docs list", "anx docs get", "anx docs content"},
	},
	{
		Name:        "cards",
		UseWhen:     "You need one board-scoped tracked work item with column, rank, assignee, and move/update operations.",
		NotFor:      "The broader topic discussion, durable knowledge, or append-only event history.",
		Examples:    []string{"implementation task", "review item", "follow-up", "blocked work"},
		RelatedRead: []string{"anx cards list", "anx cards get", "anx cards move"},
	},
	{
		Name:        "events",
		UseWhen:     "You need immutable facts, observations, decisions, or updates in an auditable sequence. Decision lifecycle events (`decision_needed`, `intervention_needed`, `decision_made`) must include `thread:<thread_id>` in refs; optional `topic:` refs are cross-links only, not a substitute for the thread anchor.",
		NotFor:      "Replacing the current durable state of a Topic, Board, Card, or Doc.",
		Examples:    []string{"decision_needed", "decision_made", "message_posted", "exception_raised"},
		RelatedRead: []string{"anx events list", "anx events explain", "anx threads timeline"},
	},
	{
		Name:        "inbox",
		UseWhen:     "You need the derived queue of what currently needs attention from the active actor's perspective.",
		NotFor:      "Durable automation contracts or historical truth.",
		Examples:    []string{"pending decisions", "exceptions", "stalled work"},
		RelatedRead: []string{"anx inbox list", "anx inbox get", "anx inbox ack"},
	},
	{
		Name:        "draft",
		UseWhen:     "You want to stage a mutation locally, inspect it, then apply it explicitly.",
		NotFor:      "Read paths or append-only event authoring.",
		Examples:    []string{"reviewable JSON writes", "document revisions without a typed proposal helper"},
		RelatedRead: []string{"anx draft create", "anx draft list", "anx draft commit"},
	},
	{
		Name:        "threads",
		UseWhen:     "You need read-only backing-thread diagnostics: timelines, raw thread records, or thread-scoped projection bundles for troubleshooting.",
		NotFor:      "Primary coordination when a Topic exists; use topics workspace instead.",
		Examples:    []string{"backing timeline", "diagnostic projection", "low-level inspection"},
		RelatedRead: []string{"anx threads list", "anx threads inspect", "anx threads workspace"},
	},
}

var inboxCategoryReference = []namedDescription{
	{Name: "action_needed", Description: "A responsible actor must decide, take direct action, or own the next step (includes prior decision and intervention queue signals)."},
	{Name: "risk_exception", Description: "Exceptions or at-risk work items that need follow-up."},
	{Name: "attention", Description: "Review or lighter operator focus (for example document attention)."},
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
		"summary":           "Quick guide to the core ANX primitives and when to use each.",
		"primitives":        primitives,
		"selection_rules":   conceptsSelectionRules(),
		"inbox_categories":  inboxCategoryReferenceMap(),
		"recommended_reads": []string{"anx help", "anx meta doc concepts", "anx meta doc agent-guide"},
	}
}

func conceptsSelectionRules() []string {
	return []string{
		"Use topics for discussion and coordination around a topic, project, incident, decision, or recurring process.",
		"Use boards for active work tracking with columns, cards, ownership, and movement.",
		"Use docs for durable context and institutional knowledge that should remain relevant over time.",
		"Use cards for individual board-scoped work items.",
		"Use events for immutable facts.",
		"Use inbox for current attention signals from the active CLI identity's perspective.",
		"Use draft when you want a local review checkpoint before a write.",
		"Use threads for read-only backing-thread diagnostics and timeline inspection, not as the default coordination surface.",
	}
}

func conceptsGuideText() string {
	var b strings.Builder
	b.WriteString("ANX concepts guide\n\n")
	b.WriteString("Use this command when you need to decide which primitive fits the use case before you start issuing writes.\n\n")
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
	b.WriteString("\nFor the fuller operating model, read `anx meta doc agent-guide`.\n")
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
