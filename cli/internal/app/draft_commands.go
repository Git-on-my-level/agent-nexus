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

	contractsclient "organization-autorunner-contracts-go-client/client"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/profile"
)

const draftVersion = 1

type persistedDraft struct {
	Version   int                    `json:"version"`
	DraftID   string                 `json:"draft_id"`
	CommandID string                 `json:"command_id"`
	Agent     string                 `json:"agent"`
	BaseURL   string                 `json:"base_url"`
	Body      map[string]any         `json:"body"`
	CreatedAt string                 `json:"created_at"`
	UpdatedAt string                 `json:"updated_at"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

func (a *App) runDraft(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: draftUsageText()}, "draft", nil
	}
	sub := strings.TrimSpace(args[0])
	switch sub {
	case "create":
		result, err := a.runDraftCreate(args[1:], cfg)
		return result, "draft create", err
	case "list":
		result, err := a.runDraftList(args[1:], cfg)
		return result, "draft list", err
	case "commit":
		result, err := a.runDraftCommit(ctx, args[1:], cfg)
		return result, "draft commit", err
	case "discard":
		result, err := a.runDraftDiscard(args[1:], cfg)
		return result, "draft discard", err
	default:
		return nil, "draft", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown draft subcommand %q", sub))
	}
}

func (a *App) runDraftCreate(args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("draft create")
	var commandFlag trackedString
	var fromFileFlag trackedString
	var draftIDFlag trackedString
	fs.Var(&commandFlag, "command", "Command ID or CLI path (for example, threads.create)")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path")
	fs.Var(&draftIDFlag, "draft-id", "Optional deterministic draft id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar draft create`")
	}

	commandID, err := resolveDraftCommandID(commandFlag.value)
	if err != nil {
		return nil, err
	}
	bodyRaw, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}
	if len(bodyRaw) == 0 {
		return nil, errnorm.Usage("invalid_request", "JSON body is required for `oar draft create` (provide stdin or --from-file)")
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
		Version:   draftVersion,
		DraftID:   draftID,
		CommandID: commandID,
		Agent:     cfg.Agent,
		BaseURL:   cfg.BaseURL,
		Body:      bodyObj,
		CreatedAt: now,
		UpdatedAt: now,
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
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar draft list`")
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
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar draft commit`")
	}

	draftsDir, err := a.draftsDir()
	if err != nil {
		return nil, err
	}
	draftPath, err := draftPathForID(draftsDir, draftID)
	if err != nil {
		return nil, err
	}
	draft, err := loadDraftFile(draftPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errnorm.Local("draft_not_found", fmt.Sprintf("draft %q was not found", draftID))
		}
		return nil, errnorm.Wrap(errnorm.KindLocal, "draft_read_failed", "failed to load draft", err)
	}
	if validation := validateDraftBody(draft.CommandID, draft.Body); len(validation) > 0 {
		return nil, errnorm.WithDetails(errnorm.Usage("draft_validation_failed", "draft body failed local validation"), map[string]any{
			"command_id": draft.CommandID,
			"errors":     validation,
		})
	}

	if targetErr := ensureDraftTargetMatchesConfig(draft, cfg); targetErr != nil {
		return nil, targetErr
	}

	commandLabel := "draft commit"
	invokeResult, invokeErr := a.invokeTypedJSON(ctx, cfg, commandLabel, draft.CommandID, nil, nil, draft.Body)
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
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar draft discard`")
	}

	draftsDir, err := a.draftsDir()
	if err != nil {
		return nil, err
	}
	draftPath, err := draftPathForID(draftsDir, draftID)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(draftPath); err != nil {
		if os.IsNotExist(err) {
			return nil, errnorm.Local("draft_not_found", fmt.Sprintf("draft %q was not found", draftID))
		}
		return nil, errnorm.Wrap(errnorm.KindLocal, "draft_discard_failed", "failed to discard draft", err)
	}
	data := map[string]any{"draft_id": draftID, "discarded": true}
	return &commandResult{Text: "Draft discarded: " + draftID, Data: data}, nil
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
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "draft-" + time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(buf), nil
}

func resolveDraftCommandID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errnorm.Usage("invalid_request", "command is required; use --command <command-id>")
	}
	if strings.Contains(raw, ".") {
		spec, ok := commandSpecByID(raw)
		if !ok {
			return "", errnorm.Usage("invalid_request", fmt.Sprintf("unknown command id %q", raw))
		}
		return spec.CommandID, nil
	}
	registryPath := mapRuntimePathToRegistryPath(raw)
	for _, spec := range contractsclient.CommandRegistry {
		if strings.TrimSpace(spec.CLIPath) == registryPath {
			return strings.TrimSpace(spec.CommandID), nil
		}
	}
	return "", errnorm.Usage("invalid_request", fmt.Sprintf("unknown command %q", raw))
}

func validateDraftBody(commandID string, body map[string]any) []string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return []string{fmt.Sprintf("unknown command id %q", commandID)}
	}
	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	if method != "POST" && method != "PATCH" && method != "PUT" {
		return []string{fmt.Sprintf("command %q is read-only and cannot be staged as a draft", commandID)}
	}
	if strings.TrimSpace(spec.InputMode) == "" || strings.TrimSpace(spec.InputMode) == "none" {
		return []string{fmt.Sprintf("command %q does not accept a request body", commandID)}
	}
	if len(spec.PathParams) > 0 {
		return []string{fmt.Sprintf("command %q requires path parameters and is not yet supported by draft create", commandID)}
	}

	required := map[string][]string{
		"threads.create":             {"thread"},
		"threads.patch":              {"patch"},
		"commitments.create":         {"commitment"},
		"commitments.patch":          {"patch"},
		"events.create":              {"event"},
		"artifacts.create":           {"artifact"},
		"inbox.ack":                  {"thread_id", "inbox_item_id"},
		"packets.work-orders.create": {"artifact", "packet"},
		"packets.receipts.create":    {"artifact", "packet"},
		"packets.reviews.create":     {"artifact", "packet"},
		"derived.rebuild":            {"actor_id"},
	}
	fields, exists := required[commandID]
	if !exists {
		return []string{fmt.Sprintf("command %q is not yet supported by draft create", commandID)}
	}
	out := make([]string, 0)
	for _, field := range fields {
		value, ok := body[field]
		if !ok {
			out = append(out, fmt.Sprintf("missing required field %q", field))
			continue
		}
		if field == "thread_id" || field == "inbox_item_id" || field == "actor_id" {
			if strings.TrimSpace(anyToString(value)) == "" {
				out = append(out, fmt.Sprintf("field %q must be a non-empty string", field))
			}
			continue
		}
		if _, ok := value.(map[string]any); !ok {
			out = append(out, fmt.Sprintf("field %q must be a JSON object", field))
		}
	}
	return out
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
	return errnorm.WithDetails(errnorm.Usage("draft_target_mismatch", "draft target does not match active --agent/--base-url"), map[string]any{
		"draft_agent":     draftAgent,
		"active_agent":    currentAgent,
		"draft_base_url":  draftBaseURL,
		"active_base_url": currentBaseURL,
	})
}

func anyToString(raw any) string {
	s, _ := raw.(string)
	return strings.TrimSpace(s)
}

func draftUsageText() string {
	return strings.TrimSpace(`
Draft commands stage write requests locally before commit.

Usage:
  oar draft create --command <command-id> [--from-file <path>]
  oar draft list
  oar draft commit <draft-id> [--keep]
  oar draft discard <draft-id>

Examples:
  cat payload.json | oar draft create --command threads.create
  oar draft commit draft-20260305T103000-a1b2c3d4e5f6
`) + "\n"
}
