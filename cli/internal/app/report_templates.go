package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/visualreport"
)

type reportTemplate struct {
	name, purpose, title, summary string
	build                         func(topic, card string) []map[string]any
}

var reportTopicRefPattern = regexp.MustCompile(`^topic:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var reportCardRefPattern = regexp.MustCompile(`^card:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var reportTemplates = []reportTemplate{
	{
		name: "workspace-overview", purpose: "Live asks, initiatives and recent activity.",
		title: "Workspace overview", summary: "Current initiatives and movement from live workspace queries.",
		build: func(topic, _ string) []map[string]any {
			return []map[string]any{
				liveReportPanel("initiatives", "live-initiatives", "Initiatives", reportWorkQuery(topic, "", map[string]any{"limit": 8, "sort": "priority"})),
				liveReportPanel("asks", "live-asks", "Open asks", reportAskQuery("", map[string]any{"limit": 8})),
				liveReportPanel("movement", "live-activity", "Recent movement", map[string]any{"limit": 8}),
			}
		},
	},
	{
		name: "initiative", purpose: "One initiative plan, linked checklist, assigned team, and answered asks.",
		title: "Initiative review", summary: "A live view of the selected initiative and its decision history.",
		build: func(topic, card string) []map[string]any {
			return []map[string]any{
				liveReportPanel("plan", "live-initiatives", "Plan and linked work", reportWorkQuery(topic, card, map[string]any{"limit": 1, "sort": "updated"})),
				liveReportPanel("decisions", "live-asks", "Decision history", reportAskQuery(card, map[string]any{"answered_only": true, "answered_within_hours": 720, "limit": 20})),
			}
		},
	},
	{
		name: "weekly-review", purpose: "What shipped, what stalled, and what was decided this week.",
		title: "Weekly review", summary: "Recent movement, plan health, and answered asks from the last week.",
		build: func(topic, card string) []map[string]any {
			return []map[string]any{
				liveReportPanel("movement", "live-activity", "What moved", map[string]any{"limit": 12}),
				liveReportPanel("initiatives", "live-initiatives", "Initiatives and plan health", reportWorkQuery(topic, card, map[string]any{"limit": 10, "sort": "updated"})),
				liveReportPanel("decisions", "live-asks", "Answered asks", reportAskQuery(card, map[string]any{"answered_only": true, "answered_within_hours": 168, "limit": 10})),
			}
		},
	},
	{
		name: "release-readiness", purpose: "Release checklist, plan blockers, and open asks.",
		title: "Release readiness", summary: "Current checklist progress and unresolved release blockers.",
		build: func(topic, card string) []map[string]any {
			return []map[string]any{
				liveReportPanel("checklist", "live-initiatives", "Release checklist", reportWorkQuery(topic, card, map[string]any{"limit": 12, "sort": "priority"})),
				liveReportPanel("blockers", "live-asks", "Open blockers and decisions", reportAskQuery(card, map[string]any{"limit": 10})),
			}
		},
	},
	{
		name: "incident-review", purpose: "Incident timeline, recent work movement, and action asks.",
		title: "Incident review", summary: "An evidence-led timeline with live movement and action asks.",
		build: func(_ string, card string) []map[string]any {
			return []map[string]any{
				liveReportPanel("movement", "live-activity", "Recent incident movement", map[string]any{"limit": 20}),
				liveReportPanel("actions", "live-asks", "Action items", reportAskQuery(card, map[string]any{"limit": 12})),
			}
		},
	},
	{
		name: "fleet-health", purpose: "Live fleet series, enrolled hosts, and authorized enrollment requests.",
		title: "Fleet health", summary: "Current fleet series and native host inventory, with authorized enrollment status.",
		build: func(_ string, _ string) []map[string]any {
			return []map[string]any{
				liveReportPanel("fleet", "live-fleet-health", "Fleet series and host inventory", map[string]any{}),
			}
		},
	},
}

func reportTemplateList() *commandResult {
	items := make([]map[string]any, 0, len(reportTemplates))
	lines := make([]string, 0, len(reportTemplates))
	for _, item := range reportTemplates {
		items = append(items, map[string]any{"name": item.name, "purpose": item.purpose})
		lines = append(lines, fmt.Sprintf("%s: %s", item.name, item.purpose))
	}
	return &commandResult{Text: strings.Join(lines, "\n"), Data: map[string]any{"templates": items}}
}

type reportInitArgs struct {
	template, topic, card string
}

func parseReportInitArgs(args []string) (reportInitArgs, error) {
	fs := newSilentFlagSet("report init")
	var template, topic, card trackedString
	fs.Var(&template, "template", "Live report template name")
	fs.Var(&topic, "topic", "Topic ref for project-scoped queries")
	fs.Var(&card, "card", "Card ref for initiative-scoped queries")
	if err := fs.Parse(args); err != nil {
		return reportInitArgs{}, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return reportInitArgs{}, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx report init`")
	}
	result := reportInitArgs{template: strings.TrimSpace(template.value), topic: strings.TrimSpace(topic.value), card: strings.TrimSpace(card.value)}
	if result.template == "" {
		result.template = "workspace-overview"
	}
	if _, ok := findReportTemplate(result.template); !ok {
		return reportInitArgs{}, errnorm.WithDetails(errnorm.Usage("invalid_report_template", "unknown report template; run `anx report templates` to list supported templates"), map[string]any{"template": result.template})
	}
	if result.topic != "" && !reportTopicRefPattern.MatchString(result.topic) {
		return reportInitArgs{}, errnorm.Usage("invalid_request", "--topic must be a topic ref")
	}
	if result.card != "" && !reportCardRefPattern.MatchString(result.card) {
		return reportInitArgs{}, errnorm.Usage("invalid_request", "--card must be a card ref")
	}
	if result.template == "initiative" && result.card == "" {
		return reportInitArgs{}, errnorm.Usage("invalid_request", "`initiative` requires --card card:<ref> so its plan and decision history use the same initiative")
	}
	return result, nil
}

func (a *App) runReportInit(args []string) (*commandResult, error) {
	parsed, err := parseReportInitArgs(args)
	if err != nil {
		return nil, err
	}
	template, _ := findReportTemplate(parsed.template)
	panels := []map[string]any{}
	for _, panel := range template.build(parsed.topic, parsed.card) {
		if visualreport.IsLive(fmt.Sprint(panel["type"])) || panel["source"] != nil {
			panels = append(panels, panel)
		}
	}
	title := template.title
	if parsed.card != "" && parsed.template == "initiative" {
		title += " — " + strings.TrimPrefix(parsed.card, "card:")
	}
	report := map[string]any{
		"kind": visualreport.Kind, "schema_version": visualreport.Version,
		"title": title, "summary": template.summary,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"projects":     []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Live workspace sources with authored narrative added by the report owner.", "outcome": "See current source data"}},
		"sources":      []any{}, "panels": panels,
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "report_template_failed", "could not encode the report template", err)
	}
	validated := visualreport.Validate(encoded)
	if !validated.Valid {
		return nil, errnorm.WithDetails(errnorm.Internal("report_template_invalid", "the selected report template does not satisfy the shared report contract"), map[string]any{"errors": validated.Errors})
	}
	pretty, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "report_template_failed", "could not format the report template", err)
	}
	return &commandResult{Text: string(pretty), Data: map[string]any{"template": parsed.template, "report": report}}, nil
}

func findReportTemplate(name string) (reportTemplate, bool) {
	for _, item := range reportTemplates {
		if item.name == name {
			return item, true
		}
	}
	return reportTemplate{}, false
}

func liveReportPanel(id, kind, title string, query map[string]any) map[string]any {
	return map[string]any{
		"id": id, "project_id": "workspace", "type": kind, "title": title,
		"author": "Agent Nexus template", "provenance": "reported", "observed_at": nil,
		"freshness": "unknown", "source_ids": []any{}, "data": query,
	}
}

func seriesReportPanel(id, kind, title string, source map[string]any) map[string]any {
	return map[string]any{
		"id": id, "project_id": "workspace", "type": kind, "title": title,
		"author": "Agent Nexus template", "provenance": "reported", "observed_at": nil,
		"freshness": "unknown", "source_ids": []any{}, "data": map[string]any{}, "source": source,
	}
}

func reportWorkQuery(topic, card string, values map[string]any) map[string]any {
	query := map[string]any{}
	for key, value := range values {
		query[key] = value
	}
	if topic != "" {
		query["project_ref"] = topic
	}
	if card != "" {
		query["card_ref"] = card
	}
	return query
}

func reportAskQuery(card string, values map[string]any) map[string]any {
	query := map[string]any{}
	for key, value := range values {
		query[key] = value
	}
	if card != "" {
		query["card_ref"] = card
	}
	return query
}
