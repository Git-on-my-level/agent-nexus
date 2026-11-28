package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

type rulingIndex struct {
	SchemaVersion int                `json:"schema_version"`
	GeneratedFrom []string           `json:"generated_from"`
	Rulings       []rulingIndexEntry `json:"rulings"`
}

type rulingIndexEntry struct {
	ID           string           `json:"id"`
	Aliases      []string         `json:"aliases"`
	Status       string           `json:"status"`
	SupersededBy string           `json:"superseded_by"`
	Supersedes   []string         `json:"supersedes"`
	Title        string           `json:"title"`
	Date         string           `json:"date"`
	Source       string           `json:"source"`
	Origin       string           `json:"origin"`
	Scope        string           `json:"scope,omitempty"`
	Topic        string           `json:"topic,omitempty"`
	Body         string           `json:"body,omitempty"`
	Quote        string           `json:"source_quote,omitempty"`
	Relations    []rulingRelation `json:"relations,omitempty"`
}

const defaultRulingImportMax = 1000

func rulingImportV1Warning() output.Warning {
	return output.Warning{
		Code:    "rulings_import_v1_defaults",
		Message: "schema_version 1 defaults scope to account, topic to import-<id>, and body and source_quote to the title",
		Details: map[string]any{"fields": []string{"scope", "topic", "body", "source_quote"}},
	}
}

func decodeRulingIndex(raw []byte, maxEntries int) (rulingIndex, error) {
	var index rulingIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return rulingIndex{}, err
	}
	if (index.SchemaVersion != 1 && index.SchemaVersion != 2) || len(index.GeneratedFrom) == 0 || index.Rulings == nil {
		return rulingIndex{}, fmt.Errorf("expected a generated schema_version 1 or 2 rulings index")
	}
	if len(index.Rulings) > maxEntries {
		return rulingIndex{}, fmt.Errorf("index has %d entries, exceeding --max-entries %d", len(index.Rulings), maxEntries)
	}
	names := map[string]bool{}
	entries := map[string]rulingIndexEntry{}
	for _, r := range index.Rulings {
		date, err := time.Parse(time.DateOnly, r.Date)
		if !validRulingDateID(r.ID) || err != nil || r.ID[2:10] != date.Format("20060102") || r.Aliases == nil || strings.TrimSpace(r.Source) == "" || utf8.RuneCountInString(r.Source) > 2048 {
			return rulingIndex{}, fmt.Errorf("invalid ruling entry %q", r.ID)
		}
		switch r.Status {
		case "active", "superseded", "revoked", "spent", "historical":
		default:
			return rulingIndex{}, fmt.Errorf("invalid status for %s", r.ID)
		}
		if index.SchemaVersion == 1 && (r.Supersedes == nil || len(r.Supersedes) > 200) {
			return rulingIndex{}, fmt.Errorf("invalid supersedes field for %s", r.ID)
		}
		switch r.Origin {
		case "rulings-file", "register", "wave3", "hostctl":
		default:
			return rulingIndex{}, fmt.Errorf("invalid origin for %s", r.ID)
		}
		if err := validateRulingAliases(r.Aliases); err != nil {
			return rulingIndex{}, err
		}
		for _, name := range append([]string{r.ID}, r.Aliases...) {
			if names[name] {
				return rulingIndex{}, fmt.Errorf("duplicate ruling ID or alias %q", name)
			}
			names[name] = true
		}
		entries[r.ID] = r
	}
	for _, r := range index.Rulings {
		seen := map[string]bool{r.ID: true}
		if r.SupersededBy != "" && (r.Status != "superseded" || r.SupersededBy == r.ID || !validRulingDateID(r.SupersededBy) || (index.SchemaVersion == 1 && entries[r.SupersededBy].ID == "")) {
			return rulingIndex{}, fmt.Errorf("invalid superseder for %s", r.ID)
		}
		if index.SchemaVersion == 2 {
			continue
		}
		for _, target := range r.Supersedes {
			if seen[target] || entries[target].ID == "" {
				return rulingIndex{}, fmt.Errorf("invalid supersession from %s to %s", r.ID, target)
			}
			seen[target] = true
		}
	}
	sort.Slice(index.Rulings, func(i, j int) bool { return index.Rulings[i].ID < index.Rulings[j].ID })
	return index, nil
}

type rulingImportRow struct {
	proposal  rulingProposal
	workspace string
}

func (index rulingIndex) importRows() ([]rulingImportRow, error) {
	rows := make([]rulingImportRow, 0, len(index.Rulings))
	for _, r := range index.Rulings {
		row, err := r.importRow(index.SchemaVersion)
		if err != nil {
			return nil, fmt.Errorf("ruling %s: %w", r.ID, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r rulingIndexEntry) importRow(version int) (rulingImportRow, error) {
	req := rulingProposal{
		ID: r.ID, Aliases: r.Aliases, Title: r.Title, Source: r.Source,
		StatusHint: r.Status, SupersededByHint: r.SupersededBy,
		Scope: r.Scope, Topic: r.Topic, Body: r.Body, Quote: r.Quote, Relations: r.Relations,
	}
	var workspace string
	if version == 1 {
		req.Scope, req.Topic, req.Body, req.Quote = "account", "import-"+strings.ToLower(r.ID), r.Title, r.Title
		req.Relations = nil
		for _, target := range r.Supersedes {
			req.Relations = append(req.Relations, rulingRelation{Kind: "supersedes", ID: target})
		}
	} else if name, ok := strings.CutPrefix(req.Scope, "workspace:"); ok && name != "" {
		workspace, req.Scope = name, "workspace"
	} else if req.Scope != "account" {
		return rulingImportRow{}, fmt.Errorf("scope must be account or workspace:<name>")
	}
	if !rulingTopicPattern.MatchString(req.Topic) {
		return rulingImportRow{}, fmt.Errorf("topic must be a slug of up to 64 lowercase letters, digits and hyphens")
	}
	if err := req.validateContent(); err != nil {
		return rulingImportRow{}, err
	}
	if err := validateRulingRelations(req.ID, req.Relations); err != nil {
		return rulingImportRow{}, err
	}
	for _, rel := range req.Relations {
		if rel.Kind == "supersedes" && (r.Status == "revoked" || r.Status == "spent") {
			return rulingImportRow{}, fmt.Errorf("retired ruling cannot supersede another ruling")
		}
	}
	return rulingImportRow{proposal: req, workspace: workspace}, nil
}

func (a *App) importRulings(ctx context.Context, cfg config.Resolved, path string, maxEntries int, dryRun bool) (*commandResult, error) {
	raw, err := a.readRulingInput(path, rulingProjectionSize)
	if err != nil {
		return nil, err
	}
	index, err := decodeRulingIndex(raw, maxEntries)
	if err != nil {
		return nil, errnorm.Usage("invalid_rulings_index", err.Error())
	}
	rows, err := index.importRows()
	if err != nil {
		return nil, errnorm.Usage("invalid_rulings_index", err.Error())
	}
	workspaces, err := a.resolveRulingWorkspaces(cfg)
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		base := workspaces.selected
		if row.workspace != "" {
			base = workspaces.aliases[row.workspace]
			if base == "" {
				return nil, errnorm.Usage("workspace_unknown", fmt.Sprintf("ruling %s targets unknown or unenrolled workspace %q; run anx config workspaces", row.proposal.ID, row.workspace))
			}
		}
		rows[i].workspace = base
	}
	var warnings []output.Warning
	if index.SchemaVersion == 1 {
		warnings = append(warnings, rulingImportV1Warning())
	}
	if dryRun {
		return previewRulingImport(rows, warnings), nil
	}
	projections, projectionWarnings, err := a.readRulingProjections(ctx, workspaces, true, false)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, projectionWarnings...)
	legacy := map[string]bool{}
	for _, projection := range projections {
		legacy[projection.BaseURL] = projection.Revision == rulingRevisionLegacy
	}
	snapshots := map[string]rulingCollection{}
	for _, row := range rows {
		if index.SchemaVersion == 2 && legacy[row.workspace] {
			return nil, errnorm.Usage("rulings_index_unavailable", fmt.Sprintf("ruling %s targets %s, which does not serve an operating ruling projection", row.proposal.ID, row.workspace))
		}
		snapshot, ok := snapshots[row.workspace]
		if !ok {
			snapshot, err = indexRulingProjections(projections, row.workspace)
			if err != nil {
				return nil, err
			}
			snapshots[row.workspace] = snapshot
			warnings = append(warnings, snapshot.warnings...)
		}
		if err := checkRulingWriteNames(snapshot, row.proposal, row.workspace); err != nil {
			return nil, err
		}
	}
	result := []any{}
	for _, row := range rows {
		sent, err := a.sendRulingProposal(ctx, workspaces.configs[row.workspace], "rulings import", row.proposal)
		if err != nil {
			return nil, fmt.Errorf("import %s after %d successful submissions (safe to retry the same index with the same workspace selection): %w", row.proposal.ID, len(result), err)
		}
		result = append(result, sent)
	}
	return &commandResult{Data: map[string]any{"rulings": result, "submitted": len(result)}, Warnings: warnings}, nil
}

func previewRulingImport(rows []rulingImportRow, warnings []output.Warning) *commandResult {
	type target struct {
		WorkspaceURL string           `json:"workspace_url"`
		Count        int              `json:"count"`
		Rows         []rulingProposal `json:"rows"`
	}
	positions := map[string]int{}
	targets := []target{}
	for _, row := range rows {
		i, ok := positions[row.workspace]
		if !ok {
			i = len(targets)
			positions[row.workspace] = i
			targets = append(targets, target{WorkspaceURL: row.workspace})
		}
		group := &targets[i]
		group.Count++
		if len(group.Rows) < 3 {
			group.Rows = append(group.Rows, row.proposal)
		}
	}
	return &commandResult{
		Data:     map[string]any{"dry_run": true, "submitted": 0, "targets": targets},
		Warnings: warnings, SkipScheduledMaintenance: true,
	}
}
