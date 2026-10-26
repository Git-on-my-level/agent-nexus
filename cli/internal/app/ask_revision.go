package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

func askReviseUsageText() string {
	return `anx ask revise <ask-ref> --from-file presentation.json [--request-key KEY] [--dry-run]
anx ask revise <ask-ref> --undo [--if-revision N] [--request-key KEY]
Read the token: anx ask presentation <ask-ref>.
presentation.json is {"if_revision":0,"presentation":{"title":"Plain question?","situation":"What happened."}}.
Only title, situation, impact, recommendation {action,why}, alternatives [{action,consequence}], if_no_answer and details may change. Null clears an optional field. Options and routing never change.
Undo restores the previous presentation as a new audited revision. Without --if-revision, undo reads the current presentation token first.
A missing request_key is derived from the ask ref and exact revision body; identical retries reuse it. Keep the file/token unchanged after an uncertain result.`
}

func (a *App) runAskPresentation(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if hasHelpToken(args) {
		return &commandResult{Text: "anx ask presentation <ask-ref>: read presentation and presentation_revision for anx ask revise"}, nil
	}
	if len(args) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx ask presentation <ask-ref>")
	}
	return a.invokeTypedJSON(ctx, cfg, "ask presentation", "asks.presentation.get", map[string]string{"ask_id": args[0]}, nil, nil)
}
func (a *App) runAskRevise(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if hasHelpToken(args) {
		return &commandResult{Text: askReviseUsageText()}, nil
	}
	leading, flags := splitLeadingPositionals(args)
	fs := newSilentFlagSet("ask revise")
	var file, key, revision trackedString
	var undo, dry trackedBool
	fs.Var(&file, "from-file", "JSON revision request file, or - for stdin")
	fs.Var(&key, "request-key", "Stable replay key")
	fs.Var(&revision, "if-revision", "Presentation revision from anx ask presentation")
	fs.Var(&undo, "undo", "Restore previous presentation")
	fs.Var(&dry, "dry-run", "Preview without writing")
	if err := fs.Parse(flags); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx ask revise <ask-ref> --from-file presentation.json | --undo")
	}
	if (file.value != "") == undo.value {
		return nil, errnorm.Usage("invalid_request", "supply exactly one of --from-file or --undo")
	}
	body := map[string]any{}
	if file.value != "" {
		raw, err := a.readBodyInput(file.value)
		if err != nil {
			return nil, err
		}
		decoded, err := decodeJSONPayload(raw)
		if err != nil {
			return nil, errnorm.Usage("invalid_request", "revision file must hold JSON")
		}
		var ok bool
		body, ok = decoded.(map[string]any)
		if !ok {
			return nil, errnorm.Usage("invalid_request", "revision file must hold an object")
		}
	}
	for k := range body {
		if k != "if_revision" && k != "presentation" && k != "request_key" {
			return nil, errnorm.Usage("invalid_request", "immutable or unknown request field: "+k)
		}
	}
	if undo.value {
		body["undo"] = true
	}
	if revision.value != "" {
		n, err := strconv.Atoi(revision.value)
		if err != nil || n < 0 {
			return nil, errnorm.Usage("invalid_request", "--if-revision must be a non-negative integer")
		}
		body["if_revision"] = n
	}
	if _, ok := body["if_revision"]; !ok {
		if !undo.value {
			return nil, errnorm.Usage("invalid_request", "if_revision is required; read it with anx ask presentation <ask-ref>")
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "ask presentation", "asks.presentation.get", map[string]string{"ask_id": ids[0]}, nil, nil)
		if err != nil {
			return nil, err
		}
		body["if_revision"] = asMap(commandResultBody(result))["presentation_revision"]
	}
	if key.value != "" {
		body["request_key"] = key.value
	}
	if _, ok := body["request_key"]; !ok {
		encoded, _ := json.Marshal(map[string]any{"ask_ref": ids[0], "body": body})
		sum := sha256.Sum256(encoded)
		body["request_key"] = "ask-revise:" + hex.EncodeToString(sum[:])
	}
	if !undo.value {
		p, ok := body["presentation"].(map[string]any)
		if !ok || len(p) == 0 {
			return nil, errnorm.Usage("invalid_request", "presentation must be a nonempty object")
		}
		allowed := map[string]bool{"title": true, "situation": true, "impact": true, "recommendation": true, "alternatives": true, "if_no_answer": true, "details": true}
		for k := range p {
			if !allowed[k] {
				return nil, errnorm.Usage("invalid_request", "immutable or unknown presentation field: "+k)
			}
		}
	}
	if strings.TrimSpace(ids[0]) == "" {
		return nil, errnorm.Usage("invalid_request", "ask ref is required")
	}
	path := map[string]string{"ask_id": ids[0]}
	if dry.value {
		return dryRunResult("ask revise", "asks.revise", path, nil, body), nil
	}
	return a.invokeTypedJSON(ctx, cfg, "ask revise", "asks.revise", path, nil, body)
}

// Shape validation runs before identity/workspace resolution on unenrolled hosts.
func preflightAskRevise(args []string) error {
	leading, flags := splitLeadingPositionals(args)
	fs := newSilentFlagSet("ask revise")
	var file, key, revision trackedString
	var undo, dry trackedBool
	fs.Var(&file, "from-file", "")
	fs.Var(&key, "request-key", "")
	fs.Var(&revision, "if-revision", "")
	fs.Var(&undo, "undo", "")
	fs.Var(&dry, "dry-run", "")
	if err := fs.Parse(flags); err != nil {
		return errnorm.Usage("invalid_flags", err.Error())
	}
	if len(append(leading, fs.Args()...)) != 1 {
		return errnorm.Usage("invalid_args", "usage: anx ask revise <ask-ref> --from-file presentation.json | --undo")
	}
	if (file.value != "") == undo.value {
		return errnorm.Usage("invalid_request", "supply exactly one of --from-file or --undo")
	}
	if revision.value != "" {
		n, err := strconv.Atoi(revision.value)
		if err != nil || n < 0 {
			return errnorm.Usage("invalid_request", "--if-revision must be a non-negative integer")
		}
	}
	return nil
}
