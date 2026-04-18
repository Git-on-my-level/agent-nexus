package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) stageProposal(commandID string, pathParams map[string]string, body map[string]any, cfg config.Resolved, meta map[string]any) (persistedDraft, string, error) {
	if validation := validateDraftBody(commandID, body); len(validation) > 0 {
		return persistedDraft{}, "", errnorm.WithDetails(errnorm.Usage("draft_validation_failed", "draft body failed local validation"), map[string]any{
			"command_id": commandID,
			"errors":     validation,
		})
	}
	if err := validateDraftPathParams(commandID, pathParams); err != nil {
		return persistedDraft{}, "", err
	}

	draftID, err := generateDraftID()
	if err != nil {
		return persistedDraft{}, "", errnorm.Wrap(errnorm.KindLocal, "draft_id_generation_failed", "failed to generate draft id", err)
	}
	draftsDir, err := a.draftsDir()
	if err != nil {
		return persistedDraft{}, "", err
	}
	if err := os.MkdirAll(draftsDir, 0o700); err != nil {
		return persistedDraft{}, "", errnorm.Wrap(errnorm.KindLocal, "draft_persist_failed", "failed to create drafts directory", err)
	}
	draftPath, err := draftPathForID(draftsDir, draftID)
	if err != nil {
		return persistedDraft{}, "", err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	draft := persistedDraft{
		Version:    draftVersion,
		DraftID:    draftID,
		CommandID:  commandID,
		Agent:      cfg.Agent,
		BaseURL:    cfg.BaseURL,
		PathParams: cloneStringMap(pathParams),
		Body:       cloneMap(body),
		CreatedAt:  now,
		UpdatedAt:  now,
		Meta:       cloneMap(meta),
	}
	if err := saveDraftFile(draftPath, draft); err != nil {
		return persistedDraft{}, "", err
	}
	return draft, draftPath, nil
}

func (a *App) commitProposal(ctx context.Context, rawProposalID string, cfg config.Resolved, allowedCommandIDs ...string) (*persistedDraft, *commandResult, error) {
	draftPath, draft, err := a.loadDraftByInput(rawProposalID)
	if err != nil {
		return nil, nil, err
	}
	if len(allowedCommandIDs) > 0 {
		allowed := map[string]struct{}{}
		for _, commandID := range allowedCommandIDs {
			allowed[strings.TrimSpace(commandID)] = struct{}{}
		}
		if _, ok := allowed[strings.TrimSpace(draft.CommandID)]; !ok {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("proposal %q targets %s; expected one of %s", rawProposalID, draft.CommandID, strings.Join(allowedCommandIDs, ", ")))
		}
	}
	if validation := validateDraftBody(draft.CommandID, draft.Body); len(validation) > 0 {
		return nil, nil, errnorm.WithDetails(errnorm.Usage("draft_validation_failed", "draft body failed local validation"), map[string]any{
			"command_id": draft.CommandID,
			"errors":     validation,
		})
	}
	if err := validateDraftPathParams(draft.CommandID, draft.PathParams); err != nil {
		return nil, nil, err
	}
	if err := ensureDraftTargetMatchesConfig(draft, cfg); err != nil {
		return nil, nil, err
	}

	invokeResult, invokeErr := a.invokeTypedJSON(ctx, cfg, "proposal apply", draft.CommandID, draft.PathParams, nil, draft.Body)
	if invokeErr != nil {
		return nil, nil, invokeErr
	}
	if removeErr := os.Remove(draftPath); removeErr != nil && !os.IsNotExist(removeErr) {
		warning := fmt.Sprintf("proposal applied, but local cleanup failed: %v", removeErr)
		text := strings.TrimSpace(invokeResult.Text)
		if text == "" {
			text = "Proposal applied."
		}
		data := map[string]any{
			"proposal_id":       draft.DraftID,
			"target_command_id": draft.CommandID,
			"applied":           true,
			"kept":              true,
			"result":            invokeResult.Data,
			"warning":           warning,
		}
		return &draft, &commandResult{Text: text + "\nWarning: " + warning, Data: data}, nil
	}

	text := strings.TrimSpace(invokeResult.Text)
	if text == "" {
		text = "Proposal applied."
	}
	data := map[string]any{
		"proposal_id":       draft.DraftID,
		"target_command_id": draft.CommandID,
		"applied":           true,
		"kept":              false,
		"result":            invokeResult.Data,
	}
	return &draft, &commandResult{Text: text + "\nProposal applied: " + draft.DraftID, Data: data}, nil
}

func (a *App) loadDraftByInput(rawProposalID string) (string, persistedDraft, error) {
	draftsDir, err := a.draftsDir()
	if err != nil {
		return "", persistedDraft{}, err
	}
	resolvedID, err := resolveDraftIDFromInput(draftsDir, rawProposalID)
	if err != nil {
		return "", persistedDraft{}, err
	}
	draftPath, err := draftPathForID(draftsDir, resolvedID)
	if err != nil {
		return "", persistedDraft{}, err
	}
	draft, err := loadDraftFile(draftPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", persistedDraft{}, errnorm.Local("draft_not_found", fmt.Sprintf("draft %q was not found", rawProposalID))
		}
		return "", persistedDraft{}, errnorm.Wrap(errnorm.KindLocal, "draft_read_failed", "failed to load draft", err)
	}
	return draftPath, draft, nil
}

func resolveDraftIDFromInput(draftsDir string, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if err := validateDraftID(raw); err != nil {
		return "", err
	}

	if path, err := draftPathForID(draftsDir, raw); err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return raw, nil
		}
	}

	entries, err := os.ReadDir(draftsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errnorm.Local("draft_not_found", fmt.Sprintf("draft %q was not found", raw))
		}
		return "", errnorm.Wrap(errnorm.KindLocal, "draft_read_failed", "failed to read drafts directory", err)
	}
	matches := make([]string, 0, 4)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		draftID := strings.TrimSuffix(entry.Name(), ".json")
		if strings.HasPrefix(draftID, raw) {
			matches = append(matches, draftID)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return "", errnorm.Local("draft_not_found", fmt.Sprintf("draft %q was not found", raw))
	case 1:
		return matches[0], nil
	default:
		return "", errnorm.Usage("ambiguous_draft_id", fmt.Sprintf("draft id prefix %q matched multiple proposals: %s", raw, strings.Join(matches, ", ")))
	}
}

func validateDraftPathParams(commandID string, pathParams map[string]string) error {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("unknown command id %q", commandID))
	}
	required := make(map[string]struct{}, len(spec.PathParams))
	for _, key := range spec.PathParams {
		required[strings.TrimSpace(key)] = struct{}{}
	}
	for key, value := range pathParams {
		key = strings.TrimSpace(key)
		if _, ok := required[key]; !ok {
			return errnorm.Usage("invalid_request", fmt.Sprintf("unexpected path parameter %q for %s", key, commandID))
		}
		if err := validateID(strings.TrimSpace(value), key); err != nil {
			return err
		}
		delete(required, key)
	}
	if len(required) == 0 {
		return nil
	}
	keys := make([]string, 0, len(required))
	for key := range required {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return errnorm.Usage("invalid_request", fmt.Sprintf("missing path parameters for %s: %s", commandID, strings.Join(keys, ", ")))
}

func proposalPreviewResult(targetCommandID string, method string, path string, pathParams map[string]string, body map[string]any, proposalID string, proposalPath string, diffText string, applyCommand string) *commandResult {
	data := map[string]any{
		"proposal_id":       proposalID,
		"proposal_path":     proposalPath,
		"target_command_id": targetCommandID,
		"method":            strings.ToUpper(strings.TrimSpace(method)),
		"path":              path,
		"path_params":       cloneStringMap(pathParams),
		"body":              cloneMap(body),
		"apply_command":     applyCommand,
	}
	if strings.TrimSpace(diffText) != "" {
		data["diff"] = map[string]any{
			"format": "unified",
			"text":   diffText,
		}
	}

	lines := []string{
		"Proposal staged successfully.",
		"Proposal ID: " + proposalID,
		"Target command: " + targetCommandID,
		"Method: " + strings.ToUpper(strings.TrimSpace(method)),
		"Path: " + path,
		"Apply with: " + applyCommand,
	}
	if strings.TrimSpace(diffText) != "" {
		lines = append(lines, "", "Diff:", diffText)
	}
	return &commandResult{Text: strings.Join(lines, "\n"), Data: data}
}

func renderUnifiedDiff(beforeLabel string, beforeText string, afterLabel string, afterText string) string {
	if beforeText == afterText {
		return "(no changes)"
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(strings.TrimRight(beforeText, "\n") + "\n"),
		B:        difflib.SplitLines(strings.TrimRight(afterText, "\n") + "\n"),
		FromFile: beforeLabel,
		ToFile:   afterLabel,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Sprintf("--- %s\n+++ %s\n(diff render failed: %v)", beforeLabel, afterLabel, err)
	}
	return strings.TrimSpace(text)
}

func prettyProposalJSON(value any) string {
	text := strings.TrimSpace(formatPrettyBody(value))
	if text == "" {
		return "{}"
	}
	return text
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
