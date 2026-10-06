package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
				if template.name == "initiative" && kind == "live-initiatives" && asMap(panel["data"])["card_ref"] != "card:launch" {
					t.Fatal("initiative plan query lost its selected card scope")
				}
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
			if seriesCount != 0 {
				t.Fatalf("%s has %d declared series panels, want throughput only on workspace-overview", template.name, seriesCount)
			}
			if template.name == "fleet-health" && (len(panels) != 1 || panels[0]["type"] != "live-fleet-health") {
				t.Fatalf("fleet health is not wired to its live fleet source: %#v", panels)
			}
		})
	}
}

func TestReportTemplateRendererFixturesMatchDefinitions(t *testing.T) {
	for _, template := range reportTemplates {
		t.Run(template.name, func(t *testing.T) {
			card := ""
			title := template.title
			if template.name == "initiative" {
				card = "card:launch"
				title += " — launch"
			}
			expected := map[string]any{
				"kind": visualreport.Kind, "schema_version": visualreport.Version,
				"title": title, "summary": template.summary,
				"generated_at": "2026-10-05T00:00:00Z",
				"projects":     []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Live workspace sources with authored narrative added by the report owner.", "outcome": "See current source data"}},
				"sources":      []any{}, "panels": template.build("topic:launch", card),
			}
			expectedBytes, err := json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			var expectedReport map[string]any
			if err := json.Unmarshal(expectedBytes, &expectedReport); err != nil {
				t.Fatal(err)
			}
			fixturePath := filepath.Join("../../../web-ui/tests/fixtures/report-templates", template.name+".json")
			raw, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read browser fixture %s: %v", fixturePath, err)
			}
			var fixture struct {
				Report       map[string]any `json:"report"`
				Observations []any          `json:"observations"`
			}
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fixture.Report, expectedReport) {
				t.Fatalf("browser fixture report differs from template definition: got %#v want %#v", fixture.Report, expectedReport)
			}
			observations := map[string]map[string]any{}
			for _, item := range fixture.Observations {
				observation, _ := item.(map[string]any)
				observations[reportStringValue(observation["id"])] = observation
			}
			for _, item := range template.build("topic:launch", card) {
				id := reportStringValue(item["id"])
				kind := reportStringValue(item["type"])
				if item["source"] == nil && !visualreport.IsLive(kind) {
					continue
				}
				observation := observations[id]
				if observation == nil || observation["type"] != kind || observation["status"] != "ok" {
					t.Fatalf("browser fixture has no successful observation for %s (%s): %#v", id, kind, observation)
				}
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

func TestReportInitDefaultsToLiveOnlyDashboard(t *testing.T) {
	initialized := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "report", "init"}))
	report := asMap(asMap(initialized["result"])["report"])
	panels := asSlice(report["panels"])
	if len(panels) != 3 {
		t.Fatalf("default panels: %#v", panels)
	}
	seen := map[string]bool{}
	for _, raw := range panels {
		panel := asMap(raw)
		seen[anyStringValue(panel["type"])] = true
	}
	for _, kind := range []string{"live-asks", "live-initiatives", "live-activity"} {
		if !seen[kind] {
			t.Fatalf("missing %s", kind)
		}
	}
	for _, template := range reportTemplates {
		result, err := (&App{}).runReportInit([]string{"--template", template.name, "--card", "card:launch"})
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range asSlice(asMap(result.Data)["report"].(map[string]any)["panels"]) {
			panel := asMap(raw)
			if !visualreport.IsLive(anyStringValue(panel["type"])) && panel["source"] == nil {
				t.Fatalf("authored template panel: %#v", panel)
			}
		}
	}
}
