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

var conceptsGuidePrimitives = []conceptsPrimitive{
	{
		Name:        "topics",
		UseWhen:     "You need an agent-facing discussion/context primitive for a project, incident, decision, recurring process, or durable work subject.",
		NotFor:      "The operator Tasks projection (use `anx work list` / `anx work get`), tracking active work status across columns, or storing long-term reference material.",
		Examples:    []string{"project discussion", "incident coordination", "decision thread", "recurring process"},
		RelatedRead: []string{"anx topics list", "anx topics get", "anx topics workspace"},
	},
	{
		Name:        "boards",
		UseWhen:     "You need an active-work view with workflow columns, ownership, ordering, and visible progress across cards.",
		NotFor:      "Operating on an individual card; use `anx cards ...` for card creation, movement, messages, assignment, revisions, resolution, and lifecycle.",
		Examples:    []string{"triage board", "release board", "initiative tracking board"},
		RelatedRead: []string{"anx boards list", "anx boards workspace", "anx cards list --board <board-ref>"},
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
		UseWhen:     "You need the canonical card store: create/list/get, body and revisions, assignees, column/rank (`cards.move`), messages, resolve/reopen, and lifecycle. Cards are the durable rows behind operator Tasks.",
		NotFor:      "The operator Tasks projection. Use `anx work list` / `anx work get` for inventory, freshness, observations, and annotations as operators see them. `work.*` is layered over the same rows, not an alias of `cards.*`.",
		Examples:    []string{"implementation task", "review item", "follow-up", "blocked work"},
		RelatedRead: []string{"anx cards list", "anx cards list --board <board-ref>", "anx cards get", "anx cards move", "anx work list"},
	},
	{
		Name:        "work",
		UseWhen:     "You need the operator Tasks projection over the same card rows: inventory and detail as operators see them, acceptance criteria, observations, and freshness. `work.create` registers a commitment (and its backing card).",
		NotFor:      "Card store writes (use `anx cards ...` for move, assign, revise, resolve, reopen, and lifecycle), discussion/context (topics), or durable knowledge (docs).",
		Examples:    []string{"operator Tasks page", "cross-source commitments", "work freshness"},
		RelatedRead: []string{"anx work list", "anx work get", "anx cards get", "anx cards move"},
	},
	{
		Name:        "events",
		UseWhen:     "You need immutable facts, messages, human-attention lifecycle events, or updates in an auditable sequence. Use `human_attention_requested` and `human_attention_responded` for operator asks, reviews, escalations, and their completion history.",
		NotFor:      "Replacing the current durable state of a Topic, Board, Card, or Doc.",
		Examples:    []string{"message_posted", "human_attention_requested", "human_attention_responded", "exception_raised"},
		RelatedRead: []string{"anx events list", "anx events explain", "anx threads timeline"},
	},
	{
		Name:        "inbox",
		UseWhen:     "A human operator needs to inspect the human attention queue (`ask`, `review`, `escalate`).",
		NotFor:      "Agent wake/attention; agents use `anx notifications`. PM decisions (`anx pm decisions create`, `pm.turns.decisions.create`) are PM conversation proposals, not operator Inbox items. Create operator Inbox items with `anx human ask|review|escalate` (`human_attention_requested` with required ordered `response_proposals`).",
		Examples:    []string{"asks", "reviews", "escalations"},
		RelatedRead: []string{"anx human ask", "anx human review", "anx human escalate"},
	},
	{
		Name:        "draft",
		UseWhen:     "You want to stage a reviewable JSON write locally, inspect it, then apply it explicitly; prefer this for risky or broad mutations and human-delegated changes.",
		NotFor:      "Read paths or append-only event authoring.",
		Examples:    []string{"reviewable JSON writes", "document revisions without a typed proposal helper"},
		RelatedRead: []string{"anx draft create", "anx draft list", "anx draft commit"},
	},
	{
		Name:        "threads",
		UseWhen:     "You need backing-thread diagnostics: timelines, raw thread records, or thread-scoped projection bundles for troubleshooting. Reads are the normal use; the two writes (`threads message`, `threads reply`) exist only for bridge/wake routing on a thread that has no topic, card or document of its own.",
		NotFor:      "Any coordination a typed subject can carry. If the subject is a topic, card or document, use `topics`/`cards`/`docs` so the message lands where an operator can see it. Threads are infrastructure, never an operator-facing noun.",
		Examples:    []string{"backing timeline", "diagnostic projection", "low-level inspection", "bridge/wake routing on an untyped thread"},
		RelatedRead: []string{"anx threads list", "anx threads inspect", "anx threads workspace"},
	},
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
		"recommended_reads": []string{"anx help", "anx meta doc concepts", "anx meta doc agent-guide", "anx meta doc profiles", "anx meta doc env"},
	}
}

func conceptsSelectionRules() []string {
	return []string{
		"Use topics for agent-facing discussion and context around a topic, project, incident, decision, or recurring process.",
		"Use boards for active work tracking with columns, cards, ownership, and movement.",
		"Use docs for durable context and institutional knowledge that should remain relevant over time.",
		"Use cards for the canonical store over card rows (create, workflow writes, revisions, lifecycle).",
		"Use work (`anx work list` / `anx work get`) for the operator Tasks projection over those same rows (freshness, observations, annotations). Layered, not a duplicate of cards.",
		"Use events for immutable facts.",
		"Use inbox only for the operator human-attention queue; agents use `anx notifications`. `anx human ask|review|escalate` is the way to put something in Inbox. A PM decision is part of a PM conversation and is not an operator request.",
		"Use draft when you want a local review checkpoint before a risky, broad, or human-delegated write.",
		"Use threads for backing-thread diagnostics and timeline inspection, never as a coordination surface; write to a thread only for bridge/wake routing when no typed subject exists.",
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
	b.WriteString("\nConfiguration and profiles:\n")
	b.WriteString("- Use profiles for local CLI identity and auth material; use `ANX_AGENT` as a per-process default for multi-agent machines.\n")
	b.WriteString("- Precedence is command flags > environment variables > profile/default marker/autodiscovery > built-in defaults.\n")
	b.WriteString("- Read next: anx meta doc profiles ; anx meta doc env ; anx config show\n")
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
