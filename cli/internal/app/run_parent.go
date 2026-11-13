package app

import (
	"context"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/runcontext"
)

func singleCardCreate(method, path string) bool {
	return method == "POST" && (path == "/work" || path == "/cards" || strings.HasPrefix(path, "/boards/") && (strings.HasSuffix(path, "/cards") || strings.HasSuffix(path, "/cards/batch")))
}

func (a *App) prepareRunParent(ctx context.Context, cfg config.Resolved, body any, batch bool) runcontext.Hint {
	object, ok := body.(map[string]any)
	if !ok {
		return runcontext.Hint{}
	}
	if a.runParent == nil {
		a.runParent = &runcontext.Resolver{Getenv: a.Getenv}
	}
	apply := a.runParent.Apply
	if batch {
		apply = a.runParent.ApplyBatch
	}
	return apply(ctx, object, func(ctx context.Context, ref string) bool {
		result, err := a.invokeRawJSON(ctx, cfg, "work get", "GET", "/work/"+url.PathEscape(ref), nil)
		return err == nil && runcontext.ReadableParent(asMap(commandResultBody(result)["work"]), ref)
	})
}

func (a *App) finishCardCreate(ctx context.Context, cfg config.Resolved, result *commandResult, hint runcontext.Hint, input map[string]any) *commandResult {
	if result == nil {
		return result
	}
	for _, w := range hint.Warnings {
		result.Warnings = append(result.Warnings, output.Warning{Code: w.Code, Message: w.Message})
	}
	body := commandResultBody(result)
	if body == nil {
		return result
	}
	item := asMap(body["work"])
	if item == nil {
		item = asMap(body["card"])
	}
	if anyString(body["redirected"]) == "unsorted" {
		ref := firstNonEmpty(anyString(item["ref"]), "<card>")
		result.Warnings = append(result.Warnings, goalNeedsHumanWarning(ref))
		result.NextActions = append(result.NextActions, action("Group it under an existing goal", "anx", "mv", ref, "--under", "<goal>"))
	}
	if cards := asSlice(body["cards"]); len(cards) > 0 {
		if hint.Parent != "" {
			body["run_parent_default"] = map[string]any{"parent_ref": hint.Parent, "label": hint.Label, "message": hint.Message()}
			for _, index := range hint.Defaulted {
				if index < len(cards) {
					if argv := hint.UndoArgv(anyString(asMap(cards[index])["ref"])); len(argv) > 0 {
						result.NextActions = append(result.NextActions, action("Ungroup the created card", argv...))
					}
				}
			}
		}
		for _, card := range cards {
			if anyString(asMap(card)["parent_ref"]) == "" {
				result.Warnings = append(result.Warnings, a.cardCreateWarnings(ctx, cfg, input, "")...)
				break
			}
		}
		return result
	}

	if item == nil {
		return result
	}

	if hint.Parent != "" {
		body["run_parent_default"] = map[string]any{"parent_ref": hint.Parent, "label": hint.Label, "message": hint.Message()}
		if argv := hint.UndoArgv(anyString(item["ref"])); len(argv) > 0 {
			result.NextActions = append(result.NextActions, action("Ungroup the created card", argv...))
		}
	}
	if hint.Parent == "" && anyString(item["parent_ref"]) == "" {
		result.Warnings = append(result.Warnings, a.cardCreateWarnings(ctx, cfg, input, anyString(item["ref"]))...)
	}
	return result
}

func (a *App) cardCreateWarnings(ctx context.Context, cfg config.Resolved, body map[string]any, exclude string) []output.Warning {
	if anyString(body["under"]) != "" {
		return nil
	}
	title := anyString(body["title"])
	if title == "" {
		title = anyString(asMap(body["card"])["title"])
	}
	warningCtx, cancel := context.WithTimeout(ctx, runcontext.LookupTimeout)
	defer cancel()
	return a.workCreateWarnings(warningCtx, cfg, title, exclude)
}

// goalNeedsHumanWarning tells an agent why its top-level create landed in
// Unsorted and what to do next.
func goalNeedsHumanWarning(ref string) output.Warning {
	return output.Warning{Code: "work_create_goal_needs_human", Message: "a human sets top-level goals, so this card was created in Unsorted; group it under an existing goal, or if a human asked for it, promote it and name them",
		Details: map[string]any{"suggest": []string{"anx", "mv", ref, "--root", "--reason", "<why>", "--requested-by", "<human>"}}}
}
