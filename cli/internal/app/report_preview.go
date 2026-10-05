package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/internal/visualreport"
)

type reportPreviewArgs struct {
	source, output string
}

func reportStringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func parseReportPreviewArgs(args []string) (reportPreviewArgs, error) {
	source, tail := popLeadingPositional(args)
	fs := newSilentFlagSet("report preview")
	var outputPath trackedString
	fs.Var(&outputPath, "output", "PNG destination")
	if err := fs.Parse(tail); err != nil {
		return reportPreviewArgs{}, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		if source != "" || len(fs.Args()) > 1 {
			return reportPreviewArgs{}, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx report preview`")
		}
		source = fs.Args()[0]
	}
	source = strings.TrimSpace(source)
	if source == "" || source == "-" {
		return reportPreviewArgs{}, errnorm.Usage("invalid_request", "usage: anx report preview <doc|file> [--output <png>]")
	}
	outputPathValue := strings.TrimSpace(outputPath.value)
	if outputPathValue == "" {
		outputPathValue = "report-preview.png"
	}
	if outputPathValue == "-" {
		return reportPreviewArgs{}, errnorm.Usage("invalid_request", "--output must be a PNG file path")
	}
	return reportPreviewArgs{source: source, output: outputPathValue}, nil
}

func (a *App) runReportPreview(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	parsed, err := parseReportPreviewArgs(args)
	if err != nil {
		return nil, err
	}
	var report map[string]any
	var observations []any
	warnings := []output.Warning{}
	if isReportDocumentRef(parsed.source) || !fileExists(parsed.source) {
		read, readErr := a.getReportDocument(ctx, cfg, parsed.source)
		if readErr != nil {
			return nil, readErr
		}
		body := commandResultBody(read)
		raw, readErr := reportBytes(reportBodyContent(body))
		if readErr != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "report_content_invalid", "could not read report document content", readErr)
		}
		validated := visualreport.Validate(raw)
		if err := reportValidationError(validated); err != nil {
			return nil, err
		}
		report, _ = validated.Report.(map[string]any)
		doc := extractNestedMap(body, "document")
		id := reportDocumentRouteID(doc, parsed.source)
		rendered, _, renderErr := a.runWorkCommand(ctx, []string{"report", "render", id}, cfg)
		if renderErr != nil {
			warnings = append(warnings, output.Warning{Code: "live_preview_unavailable", Message: "Could not read live panels; the visual preview will show them as unavailable."})
			observations = unavailableReportPanels(report, "Live panel data could not be read.")
		} else {
			observations = reportObservationPanels(commandResultBody(rendered))
			observations = completeReportObservations(report, observations, "This panel was not returned by the live query.")
		}
	} else {
		raw, readErr := a.reportInput(parsed.source)
		if readErr != nil {
			return nil, readErr
		}
		validated := visualreport.Validate(raw)
		if err := reportValidationError(validated); err != nil {
			return nil, err
		}
		report, _ = validated.Report.(map[string]any)
		materialized, materializeErr := a.invokeRawJSON(ctx, cfg, "report preview", "POST", "/reports/preview", map[string]any{"report": report})
		if materializeErr != nil {
			warnings = append(warnings, output.Warning{Code: "live_preview_unavailable", Message: "Could not read live panels; the visual preview will show them as unavailable."})
			observations = unavailableReportPanels(report, "Live panel data could not be read.")
		} else {
			observations = reportObservationPanels(commandResultBody(materialized))
			observations = completeReportObservations(report, observations, "This panel was not returned by the live query.")
		}
	}

	outputPath, _ := filepath.Abs(parsed.output)
	rendered, renderErr := renderReportPNG(ctx, report, observations, outputPath)
	if renderErr != nil {
		warnings = append(warnings, output.Warning{Code: "preview_png_unavailable", Message: "PNG rendering is unavailable; the panel summary is still shown."})
	}
	panels := summarizeReportPanels(report, observations)
	text := fmt.Sprintf("Report preview: %s\n", reportStringValue(report["title"]))
	if rendered {
		text += fmt.Sprintf("PNG: %s\n", outputPath)
	} else {
		text += "PNG: unavailable\n"
	}
	for i, raw := range panels {
		panel := asMap(raw)
		text += fmt.Sprintf("%d. %s | what: %s | source: %s | freshness: %s\n", i+1, panel["title"], panel["what"], panel["source"], panel["freshness"])
	}
	return &commandResult{Text: strings.TrimSpace(text), Warnings: warnings, Data: map[string]any{"png": outputPath, "rendered": rendered, "panels": panels}}, nil
}

func isReportDocumentRef(value string) bool {
	return strings.HasPrefix(value, "doc:") || strings.HasPrefix(value, "document:")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func reportObservationPanels(body map[string]any) []any {
	if body == nil {
		return nil
	}
	return asSlice(body["panels"])
}

func unavailableReportPanels(report map[string]any, message string) []any {
	panels := []any{}
	for _, raw := range asSlice(report["panels"]) {
		panel := asMap(raw)
		if visualreport.IsLive(reportStringValue(panel["type"])) || panel["source"] != nil {
			panels = append(panels, map[string]any{"id": panel["id"], "type": panel["type"], "status": "unavailable", "message": message, "data": map[string]any{}})
		}
	}
	return panels
}

func completeReportObservations(report map[string]any, observations []any, message string) []any {
	byID := map[string]any{}
	for _, raw := range observations {
		panel := asMap(raw)
		if id := reportStringValue(panel["id"]); id != "" {
			byID[id] = raw
		}
	}
	for _, raw := range unavailableReportPanels(report, message) {
		panel := asMap(raw)
		id := reportStringValue(panel["id"])
		if byID[id] == nil {
			observations = append(observations, raw)
		}
	}
	return observations
}

func summarizeReportPanels(report map[string]any, observations []any) []any {
	observed := map[string]map[string]any{}
	for _, raw := range observations {
		panel := asMap(raw)
		if id := reportStringValue(panel["id"]); id != "" {
			observed[id] = panel
		}
	}
	result := []any{}
	for _, raw := range asSlice(report["panels"]) {
		panel := asMap(raw)
		id := reportStringValue(panel["id"])
		kind := reportStringValue(panel["type"])
		data := asMap(panel["data"])
		observation := observed[id]
		what, source, freshness := reportPanelSummary(kind, panel, data, observation)
		result = append(result, map[string]any{"id": id, "title": reportStringValue(panel["title"]), "type": kind, "what": what, "source": source, "freshness": freshness})
	}
	return result
}

func reportPanelSummary(kind string, panel, data, observation map[string]any) (string, string, string) {
	if source := asMap(panel["source"]); len(source) > 0 {
		name := reportStringValue(source["series"])
		fresh := reportObservationFreshness(observation, "Series data was not materialized.")
		return "Live series values", "series:" + name, fresh
	}
	if visualreport.IsLive(kind) {
		what := reportStringValue(panel["title"])
		if kind == "live-initiatives" {
			what = "Initiative plans, progress, checklist and linked work"
		} else if kind == "live-asks" {
			if data["answered_only"] == true {
				what = "Answered asks and responses"
			} else {
				what = "Open asks and recent answers"
			}
		} else if kind == "live-work-mix" {
			what = "Open work grouped by " + firstNonEmpty(reportStringValue(data["group_by"]), "phase")
		} else if kind == "live-activity" {
			what = "Recent workspace movement"
		} else if kind == "live-fleet-health" {
			what = "Live fleet series, enrolled hosts and authorized enrollment requests"
		}
		query, _ := json.Marshal(data)
		return what, kind + " " + string(query), reportObservationFreshness(observation, "Live query was not materialized.")
	}
	what := reportStringValue(panel["title"])
	if text := reportStringValue(data["text"]); text != "" {
		what = text
	}
	source := "authored report content"
	if refs := asSlice(panel["source_ids"]); len(refs) > 0 {
		labels := []string{}
		for _, ref := range refs {
			labels = append(labels, reportStringValue(ref))
		}
		source = strings.Join(labels, ", ")
	}
	return what, source, firstNonEmpty(reportStringValue(panel["freshness"]), "unknown")
}

func reportObservationFreshness(observation map[string]any, missing string) string {
	if len(observation) == 0 {
		return "unknown — " + missing
	}
	status := firstNonEmpty(reportStringValue(observation["status"]), "unavailable")
	observedAt := reportStringValue(observation["observed_at"])
	if observedAt != "" {
		return status + " as of " + observedAt
	}
	return status
}

func renderReportPNG(ctx context.Context, report map[string]any, observations []any, outputPath string) (bool, error) {
	root := findReportRendererRoot()
	if root == "" {
		return false, fmt.Errorf("web UI preview renderer was not found")
	}
	temporary, err := os.MkdirTemp("", "anx-report-preview-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(temporary)
	reportPath := filepath.Join(temporary, "report.json")
	observationsPath := filepath.Join(temporary, "observations.json")
	encodedReport, err := json.Marshal(report)
	if err != nil {
		return false, err
	}
	encodedObservations, err := json.Marshal(observations)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(reportPath, encodedReport, 0o600); err != nil {
		return false, err
	}
	if err := os.WriteFile(observationsPath, encodedObservations, 0o600); err != nil {
		return false, err
	}
	script := filepath.Join(root, "scripts", "preview-visual-report.mjs")
	command := exec.CommandContext(ctx, "node", script, "--report", reportPath, "--observations", observationsPath, "--output", outputPath)
	command.Dir = root
	if out, err := command.CombinedOutput(); err != nil {
		return false, fmt.Errorf("renderer did not complete: %s", strings.TrimSpace(string(out)))
	}
	if !fileExists(outputPath) {
		return false, fmt.Errorf("renderer did not create a PNG")
	}
	return true, nil
}

func findReportRendererRoot() string {
	if explicit := strings.TrimSpace(os.Getenv("ANX_WEB_UI_DIR")); explicit != "" {
		if fileExists(filepath.Join(explicit, "scripts", "preview-visual-report.mjs")) {
			return explicit
		}
	}
	current, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(current, "web-ui")
		if fileExists(filepath.Join(candidate, "scripts", "preview-visual-report.mjs")) {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}
