package app

import (
	"encoding/json"
	"testing"

	"agent-nexus-cli/internal/visualreport"
)

func TestReportTemplatesValidateAndKeepQueriesBounded(t *testing.T) {
	if len(reportTemplates) != 6 {
		t.Fatalf("got %d templates, want 6", len(reportTemplates))
	}
	for _, template := range reportTemplates {
		t.Run(template.name, func(t *testing.T) {
			card := ""
			if template.name == "initiative" {
				card = "card:launch"
			}
			report := map[string]any{
				"kind": visualreport.Kind, "schema_version": visualreport.Version,
				"title": template.title, "summary": template.summary,
				"generated_at": "2026-10-05T00:00:00Z",
				"projects":     []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Current work", "outcome": "See source data"}},
				"sources":      []any{}, "panels": template.build("topic:launch", card),
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			result := visualreport.Validate(encoded)
			if !result.Valid {
				t.Fatalf("template is invalid: %v", result.Errors)
			}
			panels := template.build("topic:launch", card)
			liveCount := 0
			seriesCount := 0
			for _, raw := range panels {
				panel := raw
				if source := panel["source"]; source != nil {
					seriesCount++
					if template.name != "workspace-overview" || panel["type"] != "chart" {
						t.Fatalf("unexpected series panel in %s: %#v", template.name, panel)
					}
					encodedSource, _ := json.Marshal(source)
					var binding map[string]any
					if err := json.Unmarshal(encodedSource, &binding); err != nil {
						t.Fatal(err)
					}
					labels := binding["labels"].(map[string]any)
					if binding["series"] != "github-prs" || labels["status"] != "merged" || binding["range"] != "84d" || binding["agg"] != "sum" {
						t.Fatalf("throughput does not match the github-prs contract: %#v", binding)
					}
					continue
				}
				kind := reportStringValue(panel["type"])
				if !visualreport.IsLive(kind) {
					continue
				}
				liveCount++
				queryBytes, _ := json.Marshal(panel["data"])
				query, err := visualreport.ParseQuery(kind, queryBytes)
				if err != nil {
					t.Fatalf("%s query is invalid: %v", kind, err)
				}
				if query.Limit < 1 || query.Limit > 100 {
					t.Fatalf("%s has unbounded query limit %d", kind, query.Limit)
				}
			}
			if liveCount == 0 {
				t.Fatal("template has no live query")
			}
			if (template.name == "workspace-overview") != (seriesCount == 1) {
				t.Fatalf("%s has %d declared series panels, want throughput only on workspace-overview", template.name, seriesCount)
			}
		})
	}
}

func TestReportInitRequiresCardForInitiativeTemplate(t *testing.T) {
	if _, err := parseReportInitArgs([]string{"--template", "initiative"}); err == nil {
		t.Fatal("initiative template accepted no card scope")
	}
	parsed, err := parseReportInitArgs([]string{"--template", "initiative", "--card", "card:launch", "--topic", "topic:release"})
	if err != nil || parsed.card != "card:launch" || parsed.topic != "topic:release" {
		t.Fatalf("parsed=%#v err=%v", parsed, err)
	}
}
