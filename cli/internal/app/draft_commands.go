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
		if err != nil {
			return nil, "draft commit", err
		}
		return result, "draft commit", nil
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

	validators := map[string]func(map[string]any) []string{
		"threads.create":             validateDraftThreadCreate,
		"threads.patch":              validateDraftThreadPatch,
		"commitments.create":         validateDraftCommitmentCreate,
		"commitments.patch":          validateDraftCommitmentPatch,
		"events.create":              validateDraftEventCreate,
		"artifacts.create":           validateDraftArtifactCreate,
		"inbox.ack":                  validateDraftInboxAck,
		"packets.work-orders.create": validateDraftWorkOrderCreate,
		"packets.receipts.create":    validateDraftReceiptCreate,
		"packets.reviews.create":     validateDraftReviewCreate,
		"derived.rebuild":            validateDraftDerivedRebuild,
	}
	validate, exists := validators[commandID]
	if !exists {
		return []string{fmt.Sprintf("command %q is not yet supported by draft create", commandID)}
	}
	return validate(body)
}

func validateDraftThreadCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	thread, ok := requiredObjectField(body, "thread", "thread", &out)
	if !ok {
		return out
	}
	if _, exists := thread["open_commitments"]; exists {
		out = append(out, "thread.open_commitments is core-maintained and cannot be set")
	}
	requiredFields := []string{
		"title",
		"type",
		"status",
		"priority",
		"tags",
		"cadence",
		"current_summary",
		"next_actions",
		"key_artifacts",
		"provenance",
	}
	for _, field := range requiredFields {
		if _, exists := thread[field]; !exists {
			out = append(out, fmt.Sprintf("thread.%s is required", field))
		}
	}
	validateThreadFields(thread, true, "thread", &out)
	return out
}

func validateDraftThreadPatch(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	validateOptionalRFC3339(body, "if_updated_at", "if_updated_at", &out)
	patch, ok := requiredObjectField(body, "patch", "patch", &out)
	if !ok {
		return out
	}
	if len(patch) == 0 {
		out = append(out, "patch is required")
		return out
	}
	if _, exists := patch["open_commitments"]; exists {
		out = append(out, "thread.open_commitments is core-maintained and cannot be patched")
	}
	validateThreadFields(patch, false, "patch", &out)
	return out
}

func validateThreadFields(thread map[string]any, createMode bool, path string, out *[]string) {
	stringFields := map[string]bool{
		"title":           true,
		"type":            true,
		"status":          true,
		"priority":        true,
		"cadence":         true,
		"current_summary": true,
	}
	datetimeFields := map[string]bool{
		"next_check_in_at": true,
	}
	stringListFields := map[string]bool{
		"tags":             true,
		"next_actions":     true,
		"open_commitments": true,
	}
	typedRefListFields := map[string]bool{
		"key_artifacts": true,
	}

	for field, raw := range thread {
		full := path + "." + field
		switch {
		case stringFields[field]:
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if createMode && strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		case datetimeFields[field]:
			if raw == nil {
				continue
			}
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be an RFC3339 datetime string")
				continue
			}
			if _, err := time.Parse(time.RFC3339, text); err != nil {
				*out = append(*out, full+" must be an RFC3339 datetime string")
			}
		case stringListFields[field]:
			if _, ok := asStringList(raw); !ok {
				*out = append(*out, full+" must be a list of strings")
			}
		case typedRefListFields[field]:
			values, ok := asStringList(raw)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			validateTypedRefs(values, full, out)
		case field == "provenance":
			provenance, ok := raw.(map[string]any)
			if !ok {
				*out = append(*out, full+" must be an object")
				continue
			}
			validateProvenance(provenance, full, out)
		}
	}
}

func validateDraftCommitmentCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	commitment, ok := requiredObjectField(body, "commitment", "commitment", &out)
	if !ok {
		return out
	}
	requiredFields := []string{
		"thread_id",
		"title",
		"owner",
		"due_at",
		"status",
		"definition_of_done",
		"links",
		"provenance",
	}
	for _, field := range requiredFields {
		if _, exists := commitment[field]; !exists {
			out = append(out, fmt.Sprintf("commitment.%s is required", field))
		}
	}
	validateCommitmentFields(commitment, true, "commitment", &out)
	return out
}

func validateDraftCommitmentPatch(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	validateOptionalRFC3339(body, "if_updated_at", "if_updated_at", &out)
	patch, ok := requiredObjectField(body, "patch", "patch", &out)
	if !ok {
		return out
	}
	if len(patch) == 0 {
		out = append(out, "patch is required")
		return out
	}
	if _, exists := patch["thread_id"]; exists {
		out = append(out, "commitment.thread_id cannot be patched")
	}
	validateCommitmentFields(patch, false, "patch", &out)
	if refs, exists := body["refs"]; exists {
		values, ok := asStringList(refs)
		if !ok {
			out = append(out, "refs must be a list of strings")
		} else {
			validateTypedRefs(values, "refs", &out)
		}
	}
	return out
}

func validateCommitmentFields(commitment map[string]any, createMode bool, path string, out *[]string) {
	stringFields := map[string]bool{
		"thread_id": true,
		"title":     true,
		"owner":     true,
		"status":    true,
	}
	for field, raw := range commitment {
		full := path + "." + field
		switch field {
		case "due_at":
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be an RFC3339 datetime string")
				continue
			}
			if _, err := time.Parse(time.RFC3339, text); err != nil {
				*out = append(*out, full+" must be an RFC3339 datetime string")
			}
		case "definition_of_done":
			if _, ok := asStringList(raw); !ok {
				*out = append(*out, full+" must be a list of strings")
			}
		case "links":
			values, ok := asStringList(raw)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			validateTypedRefs(values, full, out)
		case "provenance":
			provenance, ok := raw.(map[string]any)
			if !ok {
				*out = append(*out, full+" must be an object")
				continue
			}
			validateProvenance(provenance, full, out)
		default:
			if !stringFields[field] {
				continue
			}
			text, ok := raw.(string)
			if !ok {
				*out = append(*out, full+" must be a string")
				continue
			}
			if createMode && strings.TrimSpace(text) == "" {
				*out = append(*out, full+" must be non-empty")
			}
		}
	}
}

func validateDraftEventCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	event, ok := requiredObjectField(body, "event", "event", &out)
	if !ok {
		return out
	}
	requiredStringField(event, "type", "event.type", true, &out)
	requiredStringField(event, "summary", "event.summary", false, &out)
	refs, ok := requiredStringListField(event, "refs", "event.refs", 0, &out)
	if ok {
		validateTypedRefs(refs, "event.refs", &out)
	}
	provenance, ok := requiredObjectField(event, "provenance", "event.provenance", &out)
	if ok {
		validateProvenance(provenance, "event.provenance", &out)
	}
	if raw, exists := event["thread_id"]; exists {
		validateNonEmptyString(raw, "event.thread_id", &out)
	}
	if raw, exists := event["payload"]; exists && raw != nil {
		if _, ok := raw.(map[string]any); !ok {
			out = append(out, "event.payload must be an object")
		}
	}
	return out
}

func validateDraftArtifactCreate(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	artifact, ok := requiredObjectField(body, "artifact", "artifact", &out)
	if !ok {
		return out
	}
	kind, hasKind := artifact["kind"].(string)
	if !hasKind || strings.TrimSpace(kind) == "" {
		out = append(out, "artifact.kind is required")
	}
	refs, ok := requiredStringListField(artifact, "refs", "artifact.refs", 0, &out)
	if ok {
		validateTypedRefs(refs, "artifact.refs", &out)
	}
	contentType := ""
	rawContentType, exists := body["content_type"]
	if !exists {
		out = append(out, "content_type is required")
	} else {
		raw, ok := rawContentType.(string)
		if !ok || strings.TrimSpace(raw) == "" {
			out = append(out, "content_type is required")
		} else {
			contentType = strings.TrimSpace(raw)
		}
	}
	content, hasContent := body["content"]
	if !hasContent || content == nil {
		out = append(out, "content is required")
	}

	kind = strings.TrimSpace(kind)
	if !isPacketKind(kind) {
		return out
	}
	if contentType != "structured" {
		out = append(out, "packet artifacts must use content_type=structured")
		return out
	}
	packet, ok := content.(map[string]any)
	if !ok {
		out = append(out, "packet artifacts must provide content as a JSON object")
		return out
	}
	validatePacketForKind(kind, artifact, packet, "content", &out)
	return out
}

func validateDraftInboxAck(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	requiredStringField(body, "thread_id", "thread_id", true, &out)
	requiredStringField(body, "inbox_item_id", "inbox_item_id", true, &out)
	return out
}

func validateDraftWorkOrderCreate(body map[string]any) []string {
	return validateDraftPacketCreate(body, "work_order")
}

func validateDraftReceiptCreate(body map[string]any) []string {
	return validateDraftPacketCreate(body, "receipt")
}

func validateDraftReviewCreate(body map[string]any) []string {
	return validateDraftPacketCreate(body, "review")
}

func validateDraftPacketCreate(body map[string]any, packetKind string) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	artifact, ok := requiredObjectField(body, "artifact", "artifact", &out)
	if !ok {
		return out
	}
	packet, ok := requiredObjectField(body, "packet", "packet", &out)
	if !ok {
		return out
	}

	if rawKind, hasKind := artifact["kind"]; hasKind {
		text, ok := rawKind.(string)
		if !ok {
			out = append(out, "artifact.kind must be a string")
		} else if strings.TrimSpace(text) != packetKind {
			out = append(out, "artifact.kind must be "+packetKind)
		}
	}
	refs, ok := requiredStringListField(artifact, "refs", "artifact.refs", 0, &out)
	if ok {
		validateTypedRefs(refs, "artifact.refs", &out)
	}
	validatePacketForKind(packetKind, artifact, packet, "packet", &out)
	return out
}

func validatePacketForKind(kind string, artifact map[string]any, packet map[string]any, path string, out *[]string) {
	rules := map[string]map[string]string{
		"work_order": {
			"work_order_id":       "string",
			"thread_id":           "string",
			"objective":           "string",
			"constraints":         "list<string>",
			"context_refs":        "list<typed_ref>",
			"acceptance_criteria": "list<string>",
			"definition_of_done":  "list<string>",
		},
		"receipt": {
			"receipt_id":            "string",
			"work_order_id":         "string",
			"thread_id":             "string",
			"outputs":               "list<typed_ref+>",
			"verification_evidence": "list<typed_ref+>",
			"changes_summary":       "string",
			"known_gaps":            "list<string>",
		},
		"review": {
			"review_id":     "string",
			"work_order_id": "string",
			"receipt_id":    "string",
			"outcome":       "string",
			"notes":         "string",
			"evidence_refs": "list<typed_ref>",
		},
	}
	fields, ok := rules[kind]
	if !ok {
		*out = append(*out, fmt.Sprintf("unsupported packet kind %q", kind))
		return
	}

	fieldOrder := []string{}
	switch kind {
	case "work_order":
		fieldOrder = []string{"work_order_id", "thread_id", "objective", "constraints", "context_refs", "acceptance_criteria", "definition_of_done"}
	case "receipt":
		fieldOrder = []string{"receipt_id", "work_order_id", "thread_id", "outputs", "verification_evidence", "changes_summary", "known_gaps"}
	case "review":
		fieldOrder = []string{"review_id", "work_order_id", "receipt_id", "outcome", "notes", "evidence_refs"}
	}
	for _, name := range fieldOrder {
		kindSpec := fields[name]
		full := path + "." + name
		value, exists := packet[name]
		if !exists {
			*out = append(*out, full+" is required")
			continue
		}
		switch kindSpec {
		case "string":
			validateNonEmptyString(value, full, out)
		case "list<string>":
			if _, ok := asStringList(value); !ok {
				*out = append(*out, full+" must be a list of strings")
			}
		case "list<typed_ref>", "list<typed_ref+>":
			values, ok := asStringList(value)
			if !ok {
				*out = append(*out, full+" must be a list of strings")
				continue
			}
			if kindSpec == "list<typed_ref+>" && len(values) == 0 {
				*out = append(*out, full+" must include at least 1 item")
			}
			validateTypedRefs(values, full, out)
		}
	}

	if kind == "review" {
		if outcomeRaw, exists := packet["outcome"]; exists {
			outcome, ok := outcomeRaw.(string)
			if ok {
				switch strings.TrimSpace(outcome) {
				case "accept", "revise", "escalate":
				default:
					*out = append(*out, "packet.outcome must be one of: accept, revise, escalate")
				}
			}
		}
	}

	idField := map[string]string{
		"work_order": "work_order_id",
		"receipt":    "receipt_id",
		"review":     "review_id",
	}[kind]
	packetID, _ := packet[idField].(string)
	packetID = strings.TrimSpace(packetID)
	if packetID != "" {
		if artifactID, hasArtifactID := artifact["id"].(string); hasArtifactID {
			artifactID = strings.TrimSpace(artifactID)
			if artifactID != "" && artifactID != packetID {
				*out = append(*out, fmt.Sprintf("packet.%s must equal artifact.id", idField))
			}
		}
	}

	artifactRefs, refsOK := asStringList(artifact["refs"])
	if !refsOK {
		return
	}
	threadID, _ := packet["thread_id"].(string)
	threadID = strings.TrimSpace(threadID)
	if threadID != "" && !containsRef(artifactRefs, "thread:"+threadID) {
		*out = append(*out, fmt.Sprintf("artifact.refs must include %q", "thread:"+threadID))
	}
	switch kind {
	case "receipt":
		workOrderID, _ := packet["work_order_id"].(string)
		workOrderID = strings.TrimSpace(workOrderID)
		if workOrderID != "" && !containsRef(artifactRefs, "artifact:"+workOrderID) {
			*out = append(*out, fmt.Sprintf("artifact.refs must include %q", "artifact:"+workOrderID))
		}
	case "review":
		workOrderID, _ := packet["work_order_id"].(string)
		workOrderID = strings.TrimSpace(workOrderID)
		if workOrderID != "" && !containsRef(artifactRefs, "artifact:"+workOrderID) {
			*out = append(*out, fmt.Sprintf("artifact.refs must include %q", "artifact:"+workOrderID))
		}
		receiptID, _ := packet["receipt_id"].(string)
		receiptID = strings.TrimSpace(receiptID)
		if receiptID != "" && !containsRef(artifactRefs, "artifact:"+receiptID) {
			*out = append(*out, fmt.Sprintf("artifact.refs must include %q", "artifact:"+receiptID))
		}
	}
}

func validateDraftDerivedRebuild(body map[string]any) []string {
	out := make([]string, 0)
	validateOptionalNonEmptyString(body, "actor_id", "actor_id", &out)
	return out
}

func requiredObjectField(body map[string]any, key string, path string, out *[]string) (map[string]any, bool) {
	raw, exists := body[key]
	if !exists || raw == nil {
		*out = append(*out, path+" is required")
		return nil, false
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		*out = append(*out, path+" must be an object")
		return nil, false
	}
	return obj, true
}

func requiredStringField(body map[string]any, key string, path string, nonEmpty bool, out *[]string) {
	raw, exists := body[key]
	if !exists {
		*out = append(*out, path+" is required")
		return
	}
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be a string")
		return
	}
	if nonEmpty && strings.TrimSpace(text) == "" {
		*out = append(*out, path+" must be non-empty")
	}
}

func requiredStringListField(body map[string]any, key string, path string, minItems int, out *[]string) ([]string, bool) {
	raw, exists := body[key]
	if !exists {
		*out = append(*out, path+" is required")
		return nil, false
	}
	values, ok := asStringList(raw)
	if !ok {
		*out = append(*out, path+" must be a list of strings")
		return nil, false
	}
	if len(values) < minItems {
		*out = append(*out, fmt.Sprintf("%s must include at least %d item", path, minItems))
		return nil, false
	}
	return values, true
}

func validateOptionalNonEmptyString(body map[string]any, key string, path string, out *[]string) {
	raw, exists := body[key]
	if !exists {
		return
	}
	validateNonEmptyString(raw, path, out)
}

func validateNonEmptyString(raw any, path string, out *[]string) {
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be a string")
		return
	}
	if strings.TrimSpace(text) == "" {
		*out = append(*out, path+" must be non-empty")
	}
}

func validateOptionalRFC3339(body map[string]any, key string, path string, out *[]string) {
	raw, exists := body[key]
	if !exists || raw == nil {
		return
	}
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be an RFC3339 datetime string")
		return
	}
	if _, err := time.Parse(time.RFC3339, text); err != nil {
		*out = append(*out, path+" must be an RFC3339 datetime string")
	}
}

func validateProvenance(provenance map[string]any, path string, out *[]string) {
	sources, ok := requiredStringListField(provenance, "sources", path+".sources", 0, out)
	if ok {
		for idx, source := range sources {
			if strings.TrimSpace(source) == "" {
				*out = append(*out, fmt.Sprintf("%s[%d] must be non-empty", path+".sources", idx))
			}
		}
	}
	if rawNotes, exists := provenance["notes"]; exists {
		if _, ok := rawNotes.(string); !ok {
			*out = append(*out, path+".notes must be a string")
		}
	}
	if rawByField, exists := provenance["by_field"]; exists {
		asMap, ok := rawByField.(map[string]any)
		if !ok {
			*out = append(*out, path+".by_field must be a map of string to list<string>")
			return
		}
		for key, rawValues := range asMap {
			if strings.TrimSpace(key) == "" {
				*out = append(*out, path+".by_field field keys must be non-empty")
				continue
			}
			values, ok := asStringList(rawValues)
			if !ok {
				*out = append(*out, fmt.Sprintf("%s.by_field.%s must be a list of strings", path, key))
				continue
			}
			for idx, value := range values {
				if strings.TrimSpace(value) == "" {
					*out = append(*out, fmt.Sprintf("%s.by_field.%s[%d] must be non-empty", path, key, idx))
				}
			}
		}
	}
}

func asStringList(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	case []any:
		out := make([]string, 0, len(values))
		for _, rawValue := range values {
			value, ok := rawValue.(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	default:
		return nil, false
	}
}

func validateTypedRefs(refs []string, path string, out *[]string) {
	for idx, ref := range refs {
		if err := validateTypedRef(ref); err != nil {
			*out = append(*out, fmt.Sprintf("%s[%d]: %s", path, idx, err.Error()))
		}
	}
}

func validateTypedRef(ref string) error {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}
	prefix := strings.TrimSpace(ref[:idx])
	value := strings.TrimSpace(ref[idx+1:])
	if prefix == "" || value == "" {
		return fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}
	return nil
}

func containsRef(refs []string, expected string) bool {
	for _, ref := range refs {
		if strings.TrimSpace(ref) == expected {
			return true
		}
	}
	return false
}

func isPacketKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "work_order", "receipt", "review":
		return true
	default:
		return false
	}
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
