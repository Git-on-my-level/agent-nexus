// Local command drafts: the `anx draft` create/list/commit/discard commands
// and the on-disk draft store they read and write. Draft body validation lives
// in draft_validation.go and draft_field_checks.go, help text in draft_help.go.

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/profile"
)

const draftVersion = 1

type persistedDraft struct {
	Version    int                    `json:"version"`
	DraftID    string                 `json:"draft_id"`
	CommandID  string                 `json:"command_id"`
	Agent      string                 `json:"agent"`
	BaseURL    string                 `json:"base_url"`
	PathParams map[string]string      `json:"path_params,omitempty"`
	Body       map[string]any         `json:"body"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
}

type draftCommandFallbackSpec struct {
	pathParams []string
}

var draftCommandFallbackSpecs = map[string]draftCommandFallbackSpec{
	"derived.rebuild": {},
}

func (a *App) runDraft(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: draftUsageText()}, "draft", nil
	}
	sub := draftSubcommandSpec.normalize(args[0])
	switch sub {
	case "create":
		result, err := a.runDraftCreate(args[1:], cfg)
		return result, "draft create", err
	case "list":
		result, err := a.runDraftList(args[1:], cfg)
		return result, "draft list", err
	case "commit":
		result, err := a.runDraftCommit(ctx, args[1:], cfg)
		if err != nil {
			return nil, "draft commit", err
		}
		return result, "draft commit", nil
	case "discard":
		result, err := a.runDraftDiscard(args[1:], cfg)
		return result, "draft discard", err
	default:
		return nil, "draft", draftSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runDraftCreate(args []string, cfg config.Resolved) (*commandResult, error) {
	filteredArgs, helpRequested := stripDraftCreateHelpFlags(args)
	if helpRequested {
		return &commandResult{Text: draftCreateHelpText(filteredArgs)}, nil
	}

	fs := newSilentFlagSet("draft create")
	var commandFlag trackedString
	var fromFileFlag trackedString
	var draftIDFlag trackedString
	fs.Var(&commandFlag, "command", "Command ID or CLI path (for example, topics.create)")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&draftIDFlag, "draft-id", "Optional deterministic draft id")
	if err := fs.Parse(filteredArgs); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx draft create`")
	}

	commandID, err := resolveDraftCommandID(commandFlag.value)
	if err != nil {
		return nil, err
	}
	if err := validateDraftCreateCommand(commandID); err != nil {
		return nil, err
	}
	bodyRaw, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}
	if len(bodyRaw) == 0 {
		return nil, errnorm.Usage("invalid_request", "JSON body is required for `anx draft create` (provide stdin or --from-file)")
	}
	parsedBodyAny, err := decodeJSONPayload(bodyRaw)
	if err != nil {
		return nil, err
	}
	bodyObj, ok := parsedBodyAny.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", "draft body must be a JSON object")
	}

	if validation := validateDraftBody(commandID, bodyObj); len(validation) > 0 {
		return nil, errnorm.WithDetails(errnorm.Usage("draft_validation_failed", "draft body failed local validation"), map[string]any{
			"command_id": commandID,
			"errors":     validation,
		})
	}

	draftID := strings.TrimSpace(draftIDFlag.value)
	if draftID == "" {
		generated, genErr := generateDraftID()
		if genErr != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "draft_id_generation_failed", "failed to generate draft id", genErr)
		}
		draftID = generated
	}
	if err := validateDraftID(draftID); err != nil {
		return nil, err
	}

	draftsDir, err := a.draftsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(draftsDir, 0o700); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "draft_persist_failed", "failed to create drafts directory", err)
	}
	draftPath, err := draftPathForID(draftsDir, draftID)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(draftPath); statErr == nil {
		return nil, errnorm.Usage("draft_exists", fmt.Sprintf("draft %q already exists", draftID))
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	draft := persistedDraft{
		Version:    draftVersion,
		DraftID:    draftID,
		CommandID:  commandID,
		Agent:      cfg.Agent,
		BaseURL:    cfg.BaseURL,
		PathParams: nil,
		Body:       bodyObj,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := saveDraftFile(draftPath, draft); err != nil {
		return nil, err
	}

	data := map[string]any{
		"draft_id":   draftID,
		"draft_path": draftPath,
		"command_id": commandID,
		"agent":      cfg.Agent,
		"base_url":   cfg.BaseURL,
		"created_at": now,
	}
	text := strings.Join([]string{
		"Draft staged successfully.",
		"Draft ID: " + draftID,
		"Command: " + commandID,
		"Path: " + draftPath,
	}, "\n")
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runDraftList(args []string, _ config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("draft list")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx draft list`")
	}

	draftsDir, err := a.draftsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(draftsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &commandResult{
				Text: "No drafts found.",
				Data: map[string]any{"drafts": []any{}},
			}, nil
		}
		return nil, errnorm.Wrap(errnorm.KindLocal, "draft_read_failed", "failed to list drafts", err)
	}

	type listedDraft struct {
		DraftID   string `json:"draft_id"`
		CommandID string `json:"command_id"`
		Agent     string `json:"agent"`
		BaseURL   string `json:"base_url"`
		Path      string `json:"path"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	listed := make([]listedDraft, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(draftsDir, entry.Name())
		draft, loadErr := loadDraftFile(path)
		if loadErr != nil {
			continue
		}
		listed = append(listed, listedDraft{
			DraftID:   draft.DraftID,
			CommandID: draft.CommandID,
			Agent:     draft.Agent,
			BaseURL:   draft.BaseURL,
			Path:      path,
			CreatedAt: draft.CreatedAt,
			UpdatedAt: draft.UpdatedAt,
		})
	}
	sort.Slice(listed, func(i, j int) bool {
		if listed[i].UpdatedAt != listed[j].UpdatedAt {
			return listed[i].UpdatedAt > listed[j].UpdatedAt
		}
		return listed[i].DraftID < listed[j].DraftID
	})
	if len(listed) == 0 {
		return &commandResult{
			Text: "No drafts found.",
			Data: map[string]any{"drafts": []any{}},
		}, nil
	}

	textLines := make([]string, 0, len(listed)+1)
	textLines = append(textLines, fmt.Sprintf("Drafts (%d):", len(listed)))
	for _, item := range listed {
		textLines = append(textLines, fmt.Sprintf("- %s [%s] updated %s", item.DraftID, item.CommandID, item.UpdatedAt))
	}
	data := map[string]any{"drafts": listed}
	return &commandResult{Text: strings.Join(textLines, "\n"), Data: data}, nil
}

func (a *App) runDraftCommit(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("draft commit")
	var draftIDFlag trackedString
	var keepFlag trackedBool
	fs.Var(&draftIDFlag, "draft-id", "Draft ID")
	fs.Var(&keepFlag, "keep", "Keep draft file after successful commit")
	preprocessed := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.TrimSpace(arg) == "--keep" {
			keepFlag.set = true
			keepFlag.value = true
			continue
		}
		preprocessed = append(preprocessed, arg)
	}
	if err := fs.Parse(preprocessed); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	draftID := strings.TrimSpace(draftIDFlag.value)
	if draftID == "" && len(positionals) > 0 {
		draftID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateDraftID(draftID); err != nil {
		return nil, err
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx draft commit`")
	}

	draftPath, draft, err := a.loadDraftByInput(draftID)
	if err != nil {
		return nil, err
	}
	if validation := validateDraftBody(draft.CommandID, draft.Body); len(validation) > 0 {
		return nil, errnorm.WithDetails(errnorm.Usage("draft_validation_failed", "draft body failed local validation"), map[string]any{
			"command_id": draft.CommandID,
			"errors":     validation,
		})
	}
	if err := validateDraftPathParams(draft.CommandID, draft.PathParams); err != nil {
		return nil, err
	}

	if targetErr := ensureDraftTargetMatchesConfig(draft, cfg); targetErr != nil {
		return nil, targetErr
	}

	commandLabel := "draft commit"
	invokeResult, invokeErr := a.invokeTypedJSON(ctx, cfg, commandLabel, draft.CommandID, draft.PathParams, nil, draft.Body)
	if invokeErr != nil {
		return nil, invokeErr
	}
	keep := keepFlag.value
	cleanupWarning := ""
	if !keep {
		if removeErr := os.Remove(draftPath); removeErr != nil && !os.IsNotExist(removeErr) {
			keep = true
			cleanupWarning = fmt.Sprintf("draft committed, but local cleanup failed: %v", removeErr)
		}
	}

	text := "Draft committed successfully."
	if strings.TrimSpace(invokeResult.Text) != "" {
		text = invokeResult.Text + "\n" + text
	}
	data := map[string]any{
		"draft_id":       draft.DraftID,
		"command_id":     draft.CommandID,
		"kept":           keep,
		"committed_data": invokeResult.Data,
	}
	if cleanupWarning != "" {
		text += "\nWarning: " + cleanupWarning
		data["warning"] = cleanupWarning
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runDraftDiscard(args []string, _ config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("draft discard")
	var draftIDFlag trackedString
	fs.Var(&draftIDFlag, "draft-id", "Draft ID")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	draftID := strings.TrimSpace(draftIDFlag.value)
	if draftID == "" && len(positionals) > 0 {
		draftID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateDraftID(draftID); err != nil {
		return nil, err
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx draft discard`")
	}

	draftsDir, err := a.draftsDir()
	if err != nil {
		return nil, err
	}
	resolvedID, err := resolveDraftIDFromInput(draftsDir, draftID)
	if err != nil {
		return nil, err
	}
	draftPath, err := draftPathForID(draftsDir, resolvedID)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(draftPath); err != nil {
		if os.IsNotExist(err) {
			return nil, errnorm.Local("draft_not_found", fmt.Sprintf("draft %q was not found", draftID))
		}
		return nil, errnorm.Wrap(errnorm.KindLocal, "draft_discard_failed", "failed to discard draft", err)
	}
	data := map[string]any{"draft_id": resolvedID, "discarded": true}
	return &commandResult{Text: "Draft discarded: " + resolvedID, Data: data}, nil
}

func (a *App) draftsDir() (string, error) {
	userHomeDir := a.UserHomeDir
	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}
	home, err := userHomeDir()
	if err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "resolve_home_failed", "failed to resolve home directory", err)
	}
	return profile.DraftsDir(home), nil
}

func loadDraftFile(path string) (persistedDraft, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return persistedDraft{}, err
	}
	var draft persistedDraft
	if err := json.Unmarshal(content, &draft); err != nil {
		return persistedDraft{}, fmt.Errorf("parse draft %s: %w", path, err)
	}
	return draft, nil
}

func saveDraftFile(path string, draft persistedDraft) error {
	encoded, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return errnorm.Wrap(errnorm.KindInternal, "json_encode_failed", "failed to encode draft", err)
	}
	encoded = append(encoded, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "draft_persist_failed", "failed to write draft file", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return errnorm.Wrap(errnorm.KindLocal, "draft_persist_failed", "failed to save draft file", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "draft_persist_failed", "failed to lock down draft permissions", err)
	}
	return nil
}

func generateDraftID() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "draft-" + hex.EncodeToString(buf), nil
}

func validateDraftID(id string) error {
	id = strings.TrimSpace(id)
	if err := validateID(id, "draft id"); err != nil {
		return err
	}
	if strings.Contains(id, "/") || strings.Contains(id, `\`) || strings.Contains(id, "..") {
		return errnorm.Usage("invalid_request", fmt.Sprintf("draft id %q contains invalid path characters", id))
	}
	return nil
}

func draftPathForID(draftsDir string, draftID string) (string, error) {
	if err := validateDraftID(draftID); err != nil {
		return "", err
	}
	base := filepath.Clean(draftsDir)
	target := filepath.Clean(filepath.Join(base, draftID+".json"))
	prefix := base + string(os.PathSeparator)
	if target != base && !strings.HasPrefix(target, prefix) {
		return "", errnorm.Usage("invalid_request", "draft path escapes drafts directory")
	}
	return target, nil
}

func ensureDraftTargetMatchesConfig(draft persistedDraft, cfg config.Resolved) error {
	draftAgent := strings.TrimSpace(draft.Agent)
	currentAgent := strings.TrimSpace(cfg.Agent)
	draftBaseURL := strings.TrimRight(strings.TrimSpace(draft.BaseURL), "/")
	currentBaseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if draftAgent == currentAgent && draftBaseURL == currentBaseURL {
		return nil
	}
	return errnorm.WithDetails(errnorm.Usage("draft_target_mismatch", "draft target does not match active --as/--base-url"), map[string]any{
		"draft_agent":     draftAgent,
		"active_agent":    currentAgent,
		"draft_base_url":  draftBaseURL,
		"active_base_url": currentBaseURL,
	})
}
