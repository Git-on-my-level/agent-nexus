package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func lookupSpecFor(resource string) resourceIDLookupSpec {
	switch resource {
	case "artifacts":
		return artifactIDLookupSpec
	case "boards":
		return boardIDLookupSpec
	case "docs":
		return documentIDLookupSpec
	case "events":
		return eventIDLookupSpec
	case "cards":
		return cardIDLookupSpec
	case "topics":
		return topicIDLookupSpec
	default:
		return resourceIDLookupSpec{}
	}
}

func (a *App) runLifecycleVerb(
	ctx context.Context,
	cfg config.Resolved,
	spec lifecycleResourceSpec,
	verb string,
	args []string,
) (*commandResult, string, error) {
	commandName := spec.resource + " " + verb
	commandID := spec.resource + "." + verb
	idField := strings.TrimSuffix(spec.idFlag, "-id") + "_id"
	lookup := lookupSpecFor(spec.resource)
	if strings.TrimSpace(lookup.listCommandID) == "" {
		return nil, commandName, errnorm.Usage("internal_error", fmt.Sprintf("missing id lookup spec for resource %q", spec.resource))
	}

	allowed := false
	for _, v := range spec.verbs {
		if v == verb {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, commandName, errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown lifecycle verb %q for resource %q", verb, spec.resource))
	}

	leadingID, rest := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var idFlag trackedString
	var fromFile trackedString
	var reasonFlag trackedString
	var actorIDFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&idFlag, spec.idFlag, "Resource id")
	fs.Var(&fromFile, "from-file", "Advanced JSON body from file or stdin with -")
	fs.Var(&reasonFlag, "reason", "Short audit string stamped on the lifecycle event")
	if verb != "purge" {
		fs.Var(&actorIDFlag, "actor-id", "Actor id")
	}
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(rest); err != nil {
		return nil, commandName, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := firstNonEmpty(strings.TrimSpace(idFlag.value), leadingID)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(id, lookup.idLabel); err != nil {
		return nil, commandName, err
	}
	if len(positionals) > 0 {
		return nil, commandName, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}

	fromFilePath := strings.TrimSpace(fromFile.value)
	var body map[string]any
	var overrides map[string]any
	jsonPayloadNonEmpty := false

	recordOverwrite := func(key string, newVal any) {
		if prev, ok := body[key]; ok && strings.TrimSpace(anyString(prev)) != "" {
			if overrides == nil {
				overrides = map[string]any{}
			}
			overrides[key] = newVal
		}
	}

	if fromFilePath != "" {
		payload, err := a.readBodyInput(fromFilePath)
		if err != nil {
			return nil, commandName, err
		}
		jsonPayloadNonEmpty = len(payload) > 0
		if !jsonPayloadNonEmpty {
			body = map[string]any{}
		} else {
			decoded, err := decodeJSONPayload(payload)
			if err != nil {
				return nil, commandName, err
			}
			bodyMap, ok := decoded.(map[string]any)
			if !ok {
				return nil, commandName, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
			}
			body = bodyMap
		}
	} else {
		body = map[string]any{}
	}

	if lifecycleTrashRequiresReasonOrJSON(spec.resource, verb) {
		if strings.TrimSpace(reasonFlag.value) == "" && !jsonPayloadNonEmpty {
			return nil, commandName, errnorm.Usage(
				"invalid_request",
				fmt.Sprintf("`anx %s` requires --reason or non-empty `--from-file` JSON body", commandName),
			)
		}
	}

	if r := strings.TrimSpace(reasonFlag.value); r != "" {
		recordOverwrite("reason", r)
		body["reason"] = r
	}

	if verb != "purge" {
		if rawActor := strings.TrimSpace(actorIDFlag.value); rawActor != "" {
			actorID, err := resolveActorIDAlias(rawActor, cfg)
			if err != nil {
				return nil, commandName, err
			}
			if actorID != "" {
				recordOverwrite("actor_id", actorID)
				body["actor_id"] = actorID
			}
		}
	}

	if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, commandName, err
	}

	dryRun := dryRunFlag.set && dryRunFlag.value
	pathParams := map[string]string{idField: id}
	if dryRun {
		return dryRunResultWithFlagOverlays(commandName, commandID, pathParams, nil, body, overrides), commandName, nil
	}
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, commandName, commandID, idField, id, lookup, nil, body)
	return result, commandName, err
}

func lifecycleTrashRequiresReasonOrJSON(resource, verb string) bool {
	if verb != "trash" {
		return false
	}
	return resource == "topics" || resource == "cards" || resource == "docs"
}
