package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func askRetireUsageText() string {
	return `anx ask retire <ask-ref> --reason TEXT --evidence REF [--evidence REF...] [--request-key KEY] [--dry-run]
anx ask retire <ask-ref> --undo [--if-retirement EVENT_REF] [--request-key KEY] [--dry-run]
Retire a stale ask as withdrawn_as_stale, recording who, why and evidence and notifying the original requester. This is not a human answer. Options and routing stay intact.
Undo reopens only the named current retirement; without --if-retirement, reads its token first. Closed subjects and expired asks cannot reopen.
Keep --request-key after an uncertain result. Otherwise the key is derived from the exact request; use a fresh key for a later intentional retirement.`
}

type askRetirementFlags struct {
	ref, reason, key, retirement string
	evidence                     []string
	undo, dry                    bool
}

func parseAskRetirement(args []string) (askRetirementFlags, error) {
	var out askRetirementFlags
	leading, flags := splitLeadingPositionals(args)
	fs := newSilentFlagSet("ask retire")
	var evidence trackedStrings
	fs.StringVar(&out.reason, "reason", "", "Why this ask is stale")
	fs.StringVar(&out.key, "request-key", "", "Stable replay key")
	fs.StringVar(&out.retirement, "if-retirement", "", "Retirement event ref for undo")
	fs.Var(&evidence, "evidence", "Evidence ref; repeatable")
	fs.BoolVar(&out.undo, "undo", false, "Reopen the current retirement")
	fs.BoolVar(&out.dry, "dry-run", false, "Preview without writing")
	if err := fs.Parse(flags); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 || strings.TrimSpace(ids[0]) == "" {
		return out, errnorm.Usage("invalid_args", "usage: anx ask retire <ask-ref> --reason TEXT --evidence REF | --undo")
	}
	out.ref = ids[0]
	out.evidence = evidence.values
	if out.undo {
		if out.reason != "" || len(out.evidence) != 0 {
			return out, errnorm.Usage("invalid_request", "undo cannot include reason or evidence")
		}
	} else if strings.TrimSpace(out.reason) == "" || len(out.evidence) == 0 || out.retirement != "" {
		return out, errnorm.Usage("invalid_request", "retire requires --reason and --evidence; --if-retirement is only for undo")
	}
	for _, ref := range out.evidence {
		if err := validateEvidenceRef(ref); err != nil {
			return out, errnorm.Usage("invalid_request", err.Error())
		}
	}
	return out, nil
}
func (a *App) runAskRetire(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if hasHelpToken(args) {
		return &commandResult{Text: askRetireUsageText()}, nil
	}
	f, err := parseAskRetirement(args)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if f.undo {
		if f.retirement == "" {
			r, e := a.invokeTypedJSON(ctx, cfg, "ask retire", "asks.get", map[string]string{"ask_id": f.ref}, nil, nil)
			if e != nil {
				return nil, e
			}
			f.retirement = anyString(asMap(commandResultBody(r))["retirement_event_ref"])
			if f.retirement == "" {
				return nil, errnorm.Usage("invalid_request", "ask has no current retirement to undo")
			}
		}
		body["undo"] = true
		body["if_retirement"] = f.retirement
	} else {
		body["reason"] = f.reason
		body["evidence"] = f.evidence
	}
	if f.key == "" {
		raw, _ := json.Marshal(map[string]any{"ask_ref": f.ref, "body": body})
		sum := sha256.Sum256(raw)
		f.key = "ask-retire:" + hex.EncodeToString(sum[:])
	}
	body["request_key"] = f.key
	path := map[string]string{"ask_id": f.ref}
	if f.dry {
		return dryRunResult("ask retire", "asks.retire", path, nil, body), nil
	}
	return a.invokeTypedJSON(ctx, cfg, "ask retire", "asks.retire", path, nil, body)
}
