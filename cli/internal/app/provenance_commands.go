package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
)

type provenanceResolverSpec struct {
	resourceType string
	commandID    string
	pathParam    string
	bodyField    string
}

type provenanceNodeRecord struct {
	Ref          string
	ResourceType string
	ResourceID   string
	Hop          int
	Payload      map[string]any
	Source       map[string]any
}

type provenanceEdgeRecord struct {
	From     string
	To       string
	Relation string
}

type provenanceMissingRef struct {
	Ref          string
	From         string
	Relation     string
	Reason       string
	ErrorCode    string
	ErrorMessage string
}

type provenanceDiscoveredRef struct {
	Ref      string
	Relation string
}

type provenanceQueueContext struct {
	From     string
	Relation string
}

type provenanceQueueItem struct {
	Ref string
	Hop int
}

var provenanceResolverByPrefix = map[string]provenanceResolverSpec{
	"event": {
		resourceType: "event",
		commandID:    "events.get",
		pathParam:    "event_id",
		bodyField:    "event",
	},
	"thread": {
		resourceType: "thread",
		commandID:    "threads.inspect",
		pathParam:    "thread_id",
		bodyField:    "thread",
	},
	"artifact": {
		resourceType: "artifact",
		commandID:    "artifacts.get",
		pathParam:    "artifact_id",
		bodyField:    "artifact",
	},
	"topic": {
		resourceType: "topic",
		commandID:    "topics.get",
		pathParam:    "topic_id",
		bodyField:    "topic",
	},
}

func (a *App) runProvenanceCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: provenanceUsageText()}, "provenance", nil
	}
	sub := provenanceSubcommandSpec.normalize(args[0])
	switch sub {
	case "walk":
		result, err := a.runProvenanceWalk(ctx, args[1:], cfg)
		return result, "provenance walk", err
	default:
		return nil, "provenance", provenanceSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runProvenanceWalk(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("provenance walk")
	var fromFlag trackedString
	var depthFlag trackedInt
	var includeEventChain trackedBool
	fs.Var(&fromFlag, "from", "Start typed ref (event:<id>|thread:<id>|artifact:<id>|topic:<id>)")
	fs.Var(&depthFlag, "depth", "Traversal depth (0 means root only)")
	fs.Var(&includeEventChain, "include-event-chain", "Include event.thread_id as provenance edges")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}

	positionals := append([]string(nil), fs.Args()...)
	startRef := strings.TrimSpace(fromFlag.value)
	if startRef == "" && len(positionals) > 0 {
		startRef = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar provenance walk`")
	}
	if strings.TrimSpace(startRef) == "" {
		return nil, errnorm.Usage("invalid_request", "--from is required (for example: --from event:event_123)")
	}
	depth := 1
	if depthFlag.set {
		depth = depthFlag.value
	}
	if depth < 0 {
		return nil, errnorm.Usage("invalid_request", "--depth must be >= 0")
	}

	_, _, canonicalStartRef, parseErr := parseTypedRef(startRef)
	if parseErr != nil {
		return nil, parseErr
	}

	queue := []provenanceQueueItem{{Ref: canonicalStartRef, Hop: 0}}
	scheduledHop := map[string]int{canonicalStartRef: 0}
	queueContexts := map[string][]provenanceQueueContext{}
	visited := map[string]struct{}{}
	unresolvedByRef := map[string]provenanceMissingRef{}
	nodesByRef := map[string]provenanceNodeRecord{}
	edgesByKey := map[string]provenanceEdgeRecord{}
	missingByKey := map[string]provenanceMissingRef{}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		contexts := queueContexts[item.Ref]
		delete(queueContexts, item.Ref)
		if _, seen := visited[item.Ref]; seen {
			continue
		}
		visited[item.Ref] = struct{}{}

		node, resolveErr := a.resolveProvenanceNode(ctx, cfg, item.Ref, item.Hop)
		if resolveErr != nil {
			normalized := errnorm.Normalize(resolveErr)
			notFound := normalized != nil && normalized.Kind == errnorm.KindRemote && normalized.Code == "not_found"
			if item.Hop == 0 {
				return nil, resolveErr
			}
			if notFound {
				unresolved := provenanceMissingRef{
					Ref:          item.Ref,
					Reason:       "not_found",
					ErrorCode:    normalized.Code,
					ErrorMessage: normalized.Message,
				}
				unresolvedByRef[item.Ref] = unresolved
				if len(contexts) == 0 {
					addProvenanceMissing(missingByKey, unresolved)
				}
				for _, contextItem := range contexts {
					addProvenanceMissing(missingByKey, provenanceMissingRef{
						Ref:          unresolved.Ref,
						From:         contextItem.From,
						Relation:     contextItem.Relation,
						Reason:       unresolved.Reason,
						ErrorCode:    unresolved.ErrorCode,
						ErrorMessage: unresolved.ErrorMessage,
					})
				}
				continue
			}
			return nil, resolveErr
		}
		nodesByRef[item.Ref] = node

		if item.Hop >= depth {
			continue
		}

		discovered := extractProvenanceRefs(node.ResourceType, node.Payload, includeEventChain.set && includeEventChain.value)
		for _, next := range discovered {
			nextRef := next.Ref
			kind, resourceID, canonicalNextRef, parseErr := parseTypedRef(next.Ref)
			if parseErr == nil {
				nextRef = canonicalNextRef
			}
			if addErr := addProvenanceEdge(edgesByKey, item.Ref, nextRef, next.Relation); addErr != nil {
				return nil, addErr
			}
			if parseErr != nil {
				addProvenanceMissing(missingByKey, provenanceMissingRef{
					Ref:      next.Ref,
					From:     item.Ref,
					Relation: next.Relation,
					Reason:   "invalid_typed_ref",
				})
				continue
			}
			spec, ok := provenanceResolverByPrefix[kind]
			if !ok {
				addProvenanceMissing(missingByKey, provenanceMissingRef{
					Ref:      canonicalNextRef,
					From:     item.Ref,
					Relation: next.Relation,
					Reason:   "unsupported_ref_type",
				})
				continue
			}
			if validateErr := validateID(resourceID, spec.resourceType+" id"); validateErr != nil {
				normalized := errnorm.Normalize(validateErr)
				missing := provenanceMissingRef{
					Ref:      canonicalNextRef,
					From:     item.Ref,
					Relation: next.Relation,
					Reason:   "invalid_ref_id",
				}
				if normalized != nil {
					missing.ErrorCode = normalized.Code
					missing.ErrorMessage = normalized.Message
				}
				addProvenanceMissing(missingByKey, missing)
				continue
			}
			if unresolved, ok := unresolvedByRef[canonicalNextRef]; ok {
				addProvenanceMissing(missingByKey, provenanceMissingRef{
					Ref:          unresolved.Ref,
					From:         item.Ref,
					Relation:     next.Relation,
					Reason:       unresolved.Reason,
					ErrorCode:    unresolved.ErrorCode,
					ErrorMessage: unresolved.ErrorMessage,
				})
				continue
			}
			if _, seen := visited[canonicalNextRef]; seen {
				continue
			}
			nextHop := item.Hop + 1
			addProvenanceQueueContext(queueContexts, canonicalNextRef, item.Ref, next.Relation)
			if scheduled, exists := scheduledHop[canonicalNextRef]; exists && scheduled <= nextHop {
				continue
			}
			scheduledHop[canonicalNextRef] = nextHop
			queue = append(queue, provenanceQueueItem{
				Ref: canonicalNextRef,
				Hop: nextHop,
			})
		}
	}

	nodes := make([]map[string]any, 0, len(nodesByRef))
	for _, node := range nodesByRef {
		nodes = append(nodes, map[string]any{
			"ref":           node.Ref,
			"resource_type": node.ResourceType,
			"resource_id":   node.ResourceID,
			"hop":           node.Hop,
			"payload":       node.Payload,
			"source":        node.Source,
		})
	}
	sort.Slice(nodes, func(i int, j int) bool {
		leftHop := intValue(nodes[i]["hop"])
		rightHop := intValue(nodes[j]["hop"])
		if leftHop != rightHop {
			return leftHop < rightHop
		}
		return anyString(nodes[i]["ref"]) < anyString(nodes[j]["ref"])
	})

	edges := make([]map[string]any, 0, len(edgesByKey))
	for _, edge := range edgesByKey {
		edges = append(edges, map[string]any{
			"from":     edge.From,
			"to":       edge.To,
			"relation": edge.Relation,
		})
	}
	sort.Slice(edges, func(i int, j int) bool {
		leftFrom := anyString(edges[i]["from"])
		rightFrom := anyString(edges[j]["from"])
		if leftFrom != rightFrom {
			return leftFrom < rightFrom
		}
		leftTo := anyString(edges[i]["to"])
		rightTo := anyString(edges[j]["to"])
		if leftTo != rightTo {
			return leftTo < rightTo
		}
		return anyString(edges[i]["relation"]) < anyString(edges[j]["relation"])
	})

	missing := make([]map[string]any, 0, len(missingByKey))
	for _, item := range missingByKey {
		row := map[string]any{
			"ref":      item.Ref,
			"from":     item.From,
			"relation": item.Relation,
			"reason":   item.Reason,
		}
		if strings.TrimSpace(item.ErrorCode) != "" {
			row["error_code"] = item.ErrorCode
		}
		if strings.TrimSpace(item.ErrorMessage) != "" {
			row["error_message"] = item.ErrorMessage
		}
		missing = append(missing, row)
	}
	sort.Slice(missing, func(i int, j int) bool {
		leftRef := anyString(missing[i]["ref"])
		rightRef := anyString(missing[j]["ref"])
		if leftRef != rightRef {
			return leftRef < rightRef
		}
		leftFrom := anyString(missing[i]["from"])
		rightFrom := anyString(missing[j]["from"])
		if leftFrom != rightFrom {
			return leftFrom < rightFrom
		}
		leftRelation := anyString(missing[i]["relation"])
		rightRelation := anyString(missing[j]["relation"])
		if leftRelation != rightRelation {
			return leftRelation < rightRelation
		}
		leftReason := anyString(missing[i]["reason"])
		rightReason := anyString(missing[j]["reason"])
		if leftReason != rightReason {
			return leftReason < rightReason
		}
		leftCode := anyString(missing[i]["error_code"])
		rightCode := anyString(missing[j]["error_code"])
		if leftCode != rightCode {
			return leftCode < rightCode
		}
		return anyString(missing[i]["error_message"]) < anyString(missing[j]["error_message"])
	})

	graph := map[string]any{
		"from":                canonicalStartRef,
		"depth":               depth,
		"include_event_chain": includeEventChain.set && includeEventChain.value,
		"nodes":               nodes,
		"edges":               edges,
		"missing_refs":        missing,
	}
	headers := map[string][]string{"Content-Type": {"application/json"}}
	return &commandResult{
		Data: graph,
		Text: formatTypedCommandText("provenance.walk", 200, headers, graph, cfg.Verbose, cfg.Headers),
	}, nil
}

func (a *App) resolveProvenanceNode(ctx context.Context, cfg config.Resolved, ref string, hop int) (provenanceNodeRecord, error) {
	kind, resourceID, canonicalRef, err := parseTypedRef(ref)
	if err != nil {
		return provenanceNodeRecord{}, err
	}
	spec, ok := provenanceResolverByPrefix[kind]
	if !ok {
		return provenanceNodeRecord{}, errnorm.Usage("invalid_request", fmt.Sprintf("unsupported provenance ref type %q", kind))
	}
	if err := validateID(resourceID, spec.resourceType+" id"); err != nil {
		return provenanceNodeRecord{}, err
	}

	pathParams := map[string]string{spec.pathParam: resourceID}
	result, invokeErr := a.invokeTypedJSON(ctx, cfg, "provenance walk", spec.commandID, pathParams, nil, nil)
	if invokeErr != nil {
		return provenanceNodeRecord{}, invokeErr
	}

	data := asMap(result.Data)
	body := asMap(data["body"])
	payload := extractNestedMap(body, spec.bodyField)
	if payload == nil {
		payload = body
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if idField := strings.TrimSpace(anyString(payload["id"])); idField == "" {
		switch spec.resourceType {
		case "thread":
			if typedID := strings.TrimSpace(anyString(payload["thread_id"])); typedID != "" {
				payload["id"] = typedID
			}
		}
	}
	resourceID = firstNonEmpty(anyString(payload["id"]), resourceID)

	return provenanceNodeRecord{
		Ref:          canonicalRef,
		ResourceType: spec.resourceType,
		ResourceID:   resourceID,
		Hop:          hop,
		Payload:      payload,
		Source: map[string]any{
			"command_id": spec.commandID,
			"method":     resolveCommandMethod(spec.commandID),
			"path":       resolveCommandPath(spec.commandID, pathParams, nil),
		},
	}, nil
}

func parseTypedRef(raw string) (kind string, value string, canonical string, err error) {
	trimmed := strings.TrimSpace(raw)
	if validateErr := validateTypedRef(trimmed); validateErr != nil {
		return "", "", "", errnorm.Usage("invalid_request", validateErr.Error())
	}
	parts := strings.SplitN(trimmed, ":", 2)
	kind = strings.ToLower(strings.TrimSpace(parts[0]))
	value = strings.TrimSpace(parts[1])
	canonical = kind + ":" + value
	return kind, value, canonical, nil
}

func extractProvenanceRefs(resourceType string, payload map[string]any, includeEventChain bool) []provenanceDiscoveredRef {
	if payload == nil {
		return nil
	}
	out := make([]provenanceDiscoveredRef, 0, 8)
	seen := map[string]struct{}{}
	appendRef := func(ref string, relation string) {
		ref = strings.TrimSpace(ref)
		relation = strings.TrimSpace(relation)
		if ref == "" || relation == "" {
			return
		}
		key := relation + "|" + ref
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, provenanceDiscoveredRef{Ref: ref, Relation: relation})
	}

	if refs, ok := asStringList(payload["refs"]); ok {
		for _, ref := range refs {
			appendRef(ref, "refs")
		}
	}

	if links, ok := asStringList(payload["links"]); ok {
		for _, ref := range links {
			appendRef(ref, "links")
		}
	}
	if keyArtifacts, ok := asStringList(payload["key_artifacts"]); ok {
		for _, ref := range keyArtifacts {
			appendRef(ref, "key_artifacts")
		}
	}

	provenance := asMap(payload["provenance"])
	if provenance != nil {
		if sources, ok := asStringList(provenance["sources"]); ok {
			for _, ref := range sources {
				if !looksLikeTypedRef(ref) {
					continue
				}
				appendRef(ref, "provenance.sources")
			}
		}
		byField := asMap(provenance["by_field"])
		if len(byField) > 0 {
			fields := make([]string, 0, len(byField))
			for key := range byField {
				fields = append(fields, key)
			}
			sort.Strings(fields)
			for _, field := range fields {
				raw := byField[field]
				if items, ok := asStringList(raw); ok {
					for _, ref := range items {
						if !looksLikeTypedRef(ref) {
							continue
						}
						appendRef(ref, "provenance.by_field."+field)
					}
					continue
				}
				if ref := strings.TrimSpace(anyString(raw)); ref != "" {
					if !looksLikeTypedRef(ref) {
						continue
					}
					appendRef(ref, "provenance.by_field."+field)
				}
			}
		}
	}

	if includeEventChain && resourceType == "event" {
		if threadID := strings.TrimSpace(anyString(payload["thread_id"])); threadID != "" {
			appendRef("thread:"+threadID, "event.thread_id")
		}
	}

	if resourceType == "topic" {
		for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
			if refs, ok := asStringList(payload[field]); ok {
				for _, ref := range refs {
					appendRef(ref, field)
				}
			}
		}
		if tid := strings.TrimSpace(anyString(payload["thread_id"])); tid != "" {
			appendRef("thread:"+tid, "topic.thread_id")
		}
	}

	sort.Slice(out, func(i int, j int) bool {
		if out[i].Relation != out[j].Relation {
			return out[i].Relation < out[j].Relation
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

func addProvenanceEdge(edges map[string]provenanceEdgeRecord, from string, to string, relation string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	relation = strings.TrimSpace(relation)
	if from == "" || to == "" || relation == "" {
		return errnorm.Usage("invalid_request", "provenance edge requires non-empty from/to/relation")
	}
	key := from + "|" + to + "|" + relation
	if _, exists := edges[key]; exists {
		return nil
	}
	edges[key] = provenanceEdgeRecord{
		From:     from,
		To:       to,
		Relation: relation,
	}
	return nil
}

func addProvenanceMissing(missing map[string]provenanceMissingRef, item provenanceMissingRef) {
	item.Ref = strings.TrimSpace(item.Ref)
	item.From = strings.TrimSpace(item.From)
	item.Relation = strings.TrimSpace(item.Relation)
	item.Reason = strings.TrimSpace(item.Reason)
	item.ErrorCode = strings.TrimSpace(item.ErrorCode)
	item.ErrorMessage = strings.TrimSpace(item.ErrorMessage)
	if item.Ref == "" || item.Reason == "" {
		return
	}
	key := item.Ref + "|" + item.From + "|" + item.Relation + "|" + item.Reason + "|" + item.ErrorCode + "|" + item.ErrorMessage
	if _, exists := missing[key]; exists {
		return
	}
	missing[key] = item
}

func addProvenanceQueueContext(contexts map[string][]provenanceQueueContext, ref string, from string, relation string) {
	ref = strings.TrimSpace(ref)
	from = strings.TrimSpace(from)
	relation = strings.TrimSpace(relation)
	if ref == "" || from == "" || relation == "" {
		return
	}
	list := contexts[ref]
	for _, entry := range list {
		if entry.From == from && entry.Relation == relation {
			return
		}
	}
	contexts[ref] = append(list, provenanceQueueContext{
		From:     from,
		Relation: relation,
	})
}

func formatProvenanceWalkSummary(graph map[string]any) string {
	lines := make([]string, 0, 12)
	lines = append(lines, "Provenance walk "+strings.TrimSpace(anyString(graph["from"])))
	lines = append(lines, fmt.Sprintf("depth: %d", intValue(graph["depth"])))
	lines = append(lines, fmt.Sprintf("nodes: %d", len(asSlice(graph["nodes"]))))
	lines = append(lines, fmt.Sprintf("edges: %d", len(asSlice(graph["edges"]))))
	lines = append(lines, fmt.Sprintf("missing_refs: %d", len(asSlice(graph["missing_refs"]))))
	return strings.Join(lines, "\n")
}

func looksLikeTypedRef(raw string) bool {
	return validateTypedRef(strings.TrimSpace(raw)) == nil
}

func provenanceUsageText() string {
	return strings.TrimSpace(`Provenance guide

Use ` + "`oar provenance walk`" + ` when you need to answer questions like:

- Why does this object exist?
- What evidence or earlier object led to it?
- What thread, artifact, event, or topic is this derived from?

Mental model

- Provenance is a graph of typed refs, not just a linear event log.
- Start from the object you trust most, then walk outward a few hops.
- Keep walks narrow at first; increase depth only when the first pass is insufficient.
- Use event-chain expansion when you specifically need event-to-event lineage, not as the default for every investigation.

Usage:
  oar provenance walk --from <typed-ref> [--depth <n>] [--include-event-chain]

Typed ref roots:
  event:<id>
  thread:<id>
  artifact:<id>
  topic:<id>

Heuristics

- Start from ` + "`event:<id>`" + ` when explaining one update or mutation.
- Start from ` + "`thread:<id>`" + ` when explaining backing-thread evidence and history.
- Start from ` + "`artifact:<id>`" + ` when tracing a file or attachment back to its source.
- Start from ` + "`topic:<id>`" + ` when explaining operator-facing topic state and linked refs.
- Prefer shallow depths like 1-3 before broader traversals.

Examples:
  oar --json provenance walk --from event:event_123 --depth 2
  oar --json provenance walk --from topic:topic_123 --depth 1
  oar provenance walk --from event:event_123 --depth 3 --include-event-chain`)
}
