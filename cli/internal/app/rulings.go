package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func rulingsUsage() string {
	return "usage: anx rulings list|show|pull|check|propose|import (see anx rulings --help)"
}

type rulingsArgs struct {
	verb, ref, surfaces, file    string
	includeRevoked, strictLegacy bool
	paths                        []string
	proposal                     rulingProposalArgs
}

func parseRulingsArgs(args []string) (rulingsArgs, error) {
	var parsed rulingsArgs
	if len(args) == 0 {
		return parsed, errnorm.Usage("invalid_args", rulingsUsage())
	}
	parsed.verb = args[0]
	fs := newSilentFlagSet("rulings " + parsed.verb)
	input := args[1:]
	switch parsed.verb {
	case "list":
		fs.BoolVar(&parsed.includeRevoked, "include-revoked", false, "Include inactive and proposed rulings")
	case "show", "pull":
	case "check":
		fs.StringVar(&parsed.surfaces, "surfaces", "", "Enforcement surface globs")
		fs.BoolVar(&parsed.strictLegacy, "strict-legacy", false, "Fail bare active legacy citations")
		// The check accepts flags before or after explicit paths.
		var options []string
		for i := 0; i < len(input); i++ {
			token := input[i]
			if token == "--" {
				parsed.paths = append(parsed.paths, input[i+1:]...)
				break
			}
			if strings.HasPrefix(token, "-") {
				options = append(options, token)
				if token == "--surfaces" || token == "-surfaces" {
					if i+1 < len(input) {
						i++
						options = append(options, input[i])
					}
				}
			} else {
				parsed.paths = append(parsed.paths, token)
			}
		}
		input = options
	case "propose":
		parsed.proposal.bind(fs)
	case "import":
		fs.StringVar(&parsed.file, "file", "", "Generated rulings index")
	default:
		return parsed, errnorm.Usage("unknown_subcommand", rulingsUsage())
	}
	if err := fs.Parse(input); err != nil {
		return parsed, errnorm.Usage("invalid_flags", err.Error())
	}
	switch {
	case parsed.verb == "show" && fs.NArg() == 1:
		parsed.ref = fs.Arg(0)
	case parsed.verb == "show" || fs.NArg() != 0:
		return parsed, errnorm.Usage("invalid_args", rulingsUsage())
	case parsed.verb == "import" && parsed.file == "":
		return parsed, errnorm.Usage("invalid_args", "usage: anx rulings import --file index.json")
	case parsed.verb == "propose":
		return parsed, parsed.proposal.validate()
	}
	return parsed, nil
}

func rulingsCommandName(args []string) string {
	if len(args) > 0 {
		return "rulings " + args[0]
	}
	return "rulings"
}

func (a *App) runRulings(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	p, err := parseRulingsArgs(args)
	if err != nil {
		return nil, err
	}
	switch p.verb {
	case "propose":
		return a.proposeRuling(ctx, cfg, p.proposal)
	case "import":
		return a.importRulings(ctx, cfg, p.file)
	}
	snapshot, err := a.collectRulings(ctx, cfg, p.verb == "pull", p.verb == "list" || p.verb == "show")
	if err != nil {
		if p.verb == "check" {
			return nil, errnorm.Usage("rulings_index_unavailable", err.Error())
		}
		return nil, err
	}
	switch p.verb {
	case "pull":
		return &commandResult{Data: map[string]any{"rulings": len(snapshot.rows), "projections": snapshot.projections}}, nil
	case "check":
		for _, projection := range snapshot.projections {
			if projection["source_revision"] != "legacy" {
				return a.checkRulings(ctx, cfg, p, snapshot)
			}
		}
		return nil, errnorm.Usage("rulings_index_unavailable", "no configured workspace serves the operating ruling projection")
	case "show":
		ruling := snapshot.names[strings.TrimPrefix(p.ref, "ruling:")]
		if ruling == nil {
			return nil, errnorm.New(errnorm.KindRemote, "ruling_not_found", "ruling not found: "+p.ref)
		}
		return a.rulingsOutput(cfg, map[string]any{"ruling": ruling}, []string{formatRuling(ruling, true)}, snapshot), nil
	default:
		rows := []map[string]any{}
		lines := []string{}
		for _, ruling := range snapshot.rows {
			if !p.includeRevoked && ruling["active"] != true {
				continue
			}
			rows = append(rows, ruling)
			lines = append(lines, formatRuling(ruling, false))
		}
		if len(lines) == 0 {
			lines = append(lines, "no active standing rulings")
		}
		// Keep the original list response path for scripts that read
		// data.body.rulings. A combined projection has no single header set.
		data := map[string]any{"rulings": rows, "status_code": 200, "headers": map[string]string{}, "body": map[string]any{"rulings": rows}}
		return a.rulingsOutput(cfg, data, lines, snapshot), nil
	}
}

func (a *App) rulingsOutput(cfg config.Resolved, data any, lines []string, snapshot rulingCollection) *commandResult {
	if cfg.JSON {
		return &commandResult{Data: data, Warnings: snapshot.warnings}
	}
	a.writeOutput(a.Stdout, strings.Join(lines, "\n")+"\n")
	for _, warning := range snapshot.warnings {
		fmt.Fprintf(a.Stderr, "warning code=%s %s\n", warning.Code, warning.Message)
	}
	return &commandResult{RawWritten: true}
}

func formatRuling(ruling map[string]any, detail bool) string {
	if anyString(ruling["kind"]) == "operating" {
		line := fmt.Sprintf("%s  %q · %s · %s/%s", anyString(ruling["ruling_id"]), anyString(ruling["question"]), anyString(ruling["status"]), anyString(ruling["scope"]), anyString(ruling["topic"]))
		if next := anyString(ruling["superseded_by"]); next != "" {
			line += " → " + next
		}
		if detail {
			line += "\n" + anyString(ruling["body"]) + "\nSource quote: " + anyString(ruling["source_quote"])
			if source := anyString(ruling["source"]); source != "" {
				line += "\nSource: " + source
			}
		}
		return line
	}
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
	return line
}
