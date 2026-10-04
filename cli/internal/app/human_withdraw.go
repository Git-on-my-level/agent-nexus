package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const humanAttentionWithdrawnEventType = "human_attention_withdrawn"

func humanWithdrawUsageText() string {
	return "Withdraw one of your open asks: anx ask withdraw <event:ask-id> --reason \"<short reason>\". The withdrawal is recorded separately from a human answer."
}

func (a *App) runAskWithdraw(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if hasHelpToken(args) {
		return &commandResult{Text: humanWithdrawUsageText()}, nil
	}
	leading, flagArgs := splitLeadingPositionals(args)
	fs := newSilentFlagSet("ask withdraw")
	var reason trackedString
	var dryRun trackedBool
	fs.Var(&reason, "reason", "Short audit reason for withdrawing the ask")
	fs.Var(&dryRun, "dry-run", "Validate and preview the withdrawal without sending it")
	if err := fs.Parse(flagArgs); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx ask withdraw <event:ask-id> --reason <text>")
	}
	askID := strings.TrimSpace(ids[0])
	askID = strings.TrimPrefix(askID, "event:")
	if err := validateID(askID, "ask event id"); err != nil {
		return nil, err
	}
	withdrawReason := strings.TrimSpace(reason.value)
	if withdrawReason == "" || len(withdrawReason) > 500 {
		return nil, errnorm.Usage("invalid_request", "--reason must contain 1 to 500 characters")
	}
	body := map[string]any{
		"request_key": "ask-withdraw:" + askID,
		"event": map[string]any{
			"type":    humanAttentionWithdrawnEventType,
			"summary": "Ask withdrawn by requester",
			"refs":    []string{"event:" + askID},
			"payload": map[string]any{
				"request_event_id":      askID,
				"request_event_ref":     "event:" + askID,
				"withdrawn_by_actor_id": strings.TrimSpace(cfg.ActorID),
				"reason":                withdrawReason,
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask.withdraw"}},
		},
	}
	if err := finalizeMutationActorID(body, cfg); err != nil {
		return nil, err
	}
	if err := validateEventsCreateInput(body, "ask withdraw"); err != nil {
		return nil, err
	}
	if dryRun.value {
		return dryRunResult("ask withdraw", "events.create", nil, nil, body), nil
	}
	result, err := a.invokeTypedJSON(ctx, cfg, "ask withdraw", "events.create", nil, nil, body)
	if err != nil {
		return nil, err
	}
	event := asMap(asMap(commandResultBody(result))["event"])
	if eventID := strings.TrimSpace(anyString(event["id"])); eventID != "" {
		asMap(result.Data)["body"] = map[string]any{
			"ask_id": "event:" + askID,
			"status": "withdrawn",
			"event":  event,
		}
		result.Text = fmt.Sprintf("ask_id=event:%s status=withdrawn", askID)
	}
	return result, nil
}
