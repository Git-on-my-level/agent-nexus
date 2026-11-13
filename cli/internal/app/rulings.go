package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// anx rulings list shows the answers a human made durable. An agent reads it
// before asking: an ask a ruling covers is answered at once and reaches
// nobody. Making and revoking a ruling is a human act, done in Decide.
func rulingsUsage() string {
	return "usage: anx rulings list [--include-revoked]"
}

func parseRulingsArgs(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "list" {
		return false, errnorm.Usage("invalid_args", rulingsUsage())
	}
	fs := newSilentFlagSet("rulings list")
	revoked := fs.Bool("include-revoked", false, "Also list rulings a human revoked")
	if err := fs.Parse(args[1:]); err != nil {
		return false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return false, errnorm.Usage("invalid_args", rulingsUsage())
	}
	return *revoked, nil
}

func (a *App) runRulings(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	includeRevoked, err := parseRulingsArgs(args)
	if err != nil {
		return nil, err
	}
	path := "/rulings"
	if includeRevoked {
		path += "?include_revoked=true"
	}
	result, err := a.invokeRawJSON(ctx, cfg, "rulings list", "GET", path, nil)
	if err != nil {
		return nil, err
	}
	rulings := asSlice(commandResultBody(result)["rulings"])
	lines := make([]string, 0, len(rulings)+1)
	for _, raw := range rulings {
		ruling := asMap(raw)
		line := fmt.Sprintf("%s  %q", anyString(ruling["ref"]), anyString(ruling["question"]))
		if anyString(ruling["kind"]) == "not_a_decision" {
			line += fmt.Sprintf(" → not a decision when %s asks: use anx tell", firstNonEmpty(anyString(ruling["requester"]), "this requester"))
		} else {
			line += fmt.Sprintf(" → %q · applies whoever asks", anyString(ruling["answer"]))
		}
		line += fmt.Sprintf(" · used %d×", intValue(ruling["uses"]))
		if ruling["active"] == false {
			line += " · revoked"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, "no standing rulings: nothing a human answered has been made durable yet")
	}
	if cfg.JSON {
		return result, nil
	}
	a.writeOutput(a.Stdout, strings.Join(lines, "\n")+"\n")
	return &commandResult{RawWritten: true}, nil
}
