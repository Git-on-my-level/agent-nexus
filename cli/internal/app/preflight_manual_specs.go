// Hand-maintained flag specs for commands whose shapes are not derivable
// from the registry or runtime flag tables.

package app

func manualPreflightFlagSpecs() map[string]map[string]preflightFlagSpec {
	boolFlag := preflightFlagSpec{kind: preflightFlagBool}
	valueFlag := preflightFlagSpec{kind: preflightFlagString}
	lifecycle := map[string]preflightFlagSpec{
		"include-archived": boolFlag,
		"archived-only":    boolFlag,
		"include-trashed":  boolFlag,
		"trashed-only":     boolFlag,
	}
	listFlags := map[string]preflightFlagSpec{
		"state":   valueFlag,
		"q":       valueFlag,
		"limit":   valueFlag,
		"cursor":  valueFlag,
		"full-id": boolFlag,
	}
	merge := func(maps ...map[string]preflightFlagSpec) map[string]preflightFlagSpec {
		out := map[string]preflightFlagSpec{}
		for _, flags := range maps {
			for name, spec := range flags {
				out[name] = spec
			}
		}
		return out
	}
	return map[string]map[string]preflightFlagSpec{
		"move card": {
			"to": valueFlag, "connection-map": valueFlag, "dry-run": boolFlag,
		},
		"move topic": {
			"to": valueFlag, "connection-map": valueFlag, "dry-run": boolFlag,
		},
		"api call": {
			"method":    valueFlag,
			"path":      valueFlag,
			"from-file": valueFlag,
			"header":    valueFlag,
		},
		"auth invites create": {
			"kind": valueFlag,
		},
		"auth invites revoke": {
			"invite-id": valueFlag,
		},
		"auth principals list": {
			"limit":        valueFlag,
			"cursor":       valueFlag,
			"taggable":     boolFlag,
			"handles-only": boolFlag,
		},
		"auth audit list": {
			"limit":  valueFlag,
			"cursor": valueFlag,
		},
		"notifications list": {
			"status": valueFlag,
			"order":  valueFlag,
		},
		"notifications read": {
			"wakeup-id": valueFlag,
		},
		"notifications dismiss": {
			"wakeup-id": valueFlag,
		},
		"secret create": {
			"from-stdin":  boolFlag,
			"description": valueFlag,
		},
		"secret get": {
			"reveal": boolFlag,
		},
		"secret update": {
			"from-stdin":  boolFlag,
			"description": valueFlag,
		},
		"secret exec": {
			"secret": valueFlag,
		},
		"import scan": {
			"input":                valueFlag,
			"out":                  valueFlag,
			"max-preview-bytes":    valueFlag,
			"max-text-cache-bytes": valueFlag,
		},
		"import dedupe": {
			"inventory": valueFlag,
			"out":       valueFlag,
		},
		"import plan": {
			"inventory":           valueFlag,
			"dedupe":              valueFlag,
			"out":                 valueFlag,
			"source-name":         valueFlag,
			"collector-threshold": valueFlag,
		},
		"import apply": {
			"plan":    valueFlag,
			"out":     valueFlag,
			"execute": boolFlag,
		},
		"install skill": {
			"path":       valueFlag,
			"write-file": valueFlag,
			"force":      boolFlag,
		},
		"boards patch": {
			"board-id":      valueFlag,
			"from-file":     valueFlag,
			"role":          valueFlag,
			"if-updated-at": {kind: preflightFlagString, requireNonEmpty: true},
			"dry-run":       boolFlag,
		},
		"topics list": merge(listFlags, lifecycle),
		"topics patch": {
			"topic-id":      valueFlag,
			"from-file":     valueFlag,
			"title":         valueFlag,
			"summary":       valueFlag,
			"if-updated-at": valueFlag,
			"actor-id":      valueFlag,
			"dry-run":       boolFlag,
		},
		// Lifecycle verbs (archive/unarchive/trash/restore/purge) for topics,
		// boards, docs, cards, artifacts, and events are derived from
		// `lifecycleResourceSpecs()` in lifecycle_spec.go. Add overrides here
		// only when a parser exposes a flag outside the standard convention.
		"docs list": merge(map[string]preflightFlagSpec{
			"thread-id": valueFlag,
			"q":         valueFlag,
			"tag":       valueFlag,
			"knowledge": boolFlag,
			"limit":     valueFlag,
			"cursor":    valueFlag,
		}, lifecycle),
		"boards list": merge(listFlags, map[string]preflightFlagSpec{
			"owner": valueFlag,
		}, lifecycle),
		"actors list": {
			"q":      valueFlag,
			"limit":  valueFlag,
			"cursor": valueFlag,
		},
		"events get": {
			"id":       valueFlag,
			"event-id": valueFlag,
		},
		"events list": merge(map[string]preflightFlagSpec{
			"thread-id":     valueFlag,
			"type":          valueFlag,
			"types":         valueFlag,
			"event-group":   valueFlag,
			"backing-scope": valueFlag,
			"actor-id":      valueFlag,
			"mine":          boolFlag,
			"full-id":       boolFlag,
			"max-events":    valueFlag,
			"max":           valueFlag,
		}, lifecycle),
		"events explain": {
			"type": valueFlag,
		},
		"inbox summary": {"limit": {kind: preflightFlagString}},
		"inbox list": {
			"thread-id": valueFlag,
			"type":      valueFlag,
			"full-id":   boolFlag,
			"status":    valueFlag,
			"unread":    boolFlag,
			"limit":     valueFlag,
			"cursor":    valueFlag,
		},
		"inbox respond": {
			"from-file":     valueFlag,
			"inbox-item-id": valueFlag,
			"response-text": valueFlag,
			"outcome":       valueFlag,
			"notify-mode":   valueFlag,
			"actor-id":      valueFlag,
		},
		"topics messages": lifecycle,
		"docs messages":   lifecycle,
		"cards messages":  lifecycle,
		"docs search": {
			"q":         valueFlag,
			"tag":       valueFlag,
			"host":      valueFlag,
			"knowledge": boolFlag,
			"limit":     valueFlag,
			"cursor":    valueFlag,
		},
		"docs put": {
			"title":       valueFlag,
			"source":      valueFlag,
			"tags":        valueFlag,
			"hosts":       valueFlag,
			"verified-at": valueFlag,
			"handle":      valueFlag,
			"body":        valueFlag,
			"body-file":   valueFlag,
			"actor-id":    valueFlag,
		},
		"docs ingest": {
			"source":      valueFlag,
			"tags":        valueFlag,
			"hosts":       valueFlag,
			"verified-at": valueFlag,
			"actor-id":    valueFlag,
		},
		"docs get": {
			"document-id": valueFlag,
			"format":      valueFlag,
		},
		"docs comment": {
			"document-id": valueFlag,
			"body":        valueFlag,
			"reply-to":    valueFlag,
			"actor-id":    valueFlag,
		},
		"docs comments": {
			"document-id": valueFlag,
			"limit":       valueFlag,
			"cursor":      valueFlag,
		},
		"docs comments reply": {
			"document-id": valueFlag,
			"comment-id":  valueFlag,
			"body":        valueFlag,
			"actor-id":    valueFlag,
		},
		"docs comments edit": {
			"document-id": valueFlag,
			"comment-id":  valueFlag,
			"body":        valueFlag,
			"actor-id":    valueFlag,
		},
		"docs comments delete": {
			"document-id": valueFlag,
			"comment-id":  valueFlag,
			"actor-id":    valueFlag,
		},
		"docs revise": {
			"document-id": valueFlag,
			"proposal-id": valueFlag,
			"from-file":   valueFlag,
			"body-file":   valueFlag,
			"actor-id":    valueFlag,
			"apply":       boolFlag,
			"propose":     boolFlag,
		},
		"cards create": {
			"under": valueFlag, "after": valueFlag, "top-level": boolFlag,
			"handle":              {kind: preflightFlagString, validate: validateCardHandle},
			"board":               valueFlag,
			"board-id":            valueFlag,
			"title":               valueFlag,
			"body":                valueFlag,
			"body-file":           valueFlag,
			"from-file":           valueFlag,
			"actor-id":            valueFlag,
			"request-key":         valueFlag,
			"if-board-updated-at": valueFlag,
			"column":              valueFlag,
			"topic":               valueFlag,
			"document-ref":        valueFlag,
			"risk":                valueFlag,
			"due-at":              valueFlag,
			"before-card-id":      valueFlag,
			"after-card-id":       valueFlag,
			"assignee-ref":        valueFlag,
			"ref":                 valueFlag,
			"done":                valueFlag,
			"dry-run":             boolFlag,
		},
		"cards patch": {
			"card-id":       valueFlag,
			"from-file":     valueFlag,
			"title":         valueFlag,
			"summary":       valueFlag,
			"column-key":    valueFlag,
			"if-updated-at": valueFlag,
			"actor-id":      valueFlag,
		},
		"cards revise": {"card-id": valueFlag, "body-file": valueFlag, "from-file": valueFlag},
		"cards move": {
			"board": valueFlag, "reason": valueFlag,
			"card-id":             valueFlag,
			"column":              valueFlag,
			"if-board-updated-at": valueFlag,
			"actor-id":            valueFlag,
			"from-file":           valueFlag,
			"dry-run":             boolFlag,
		},
		"cards assign": {"card-id": valueFlag},
		"cards resolve": {
			"card-id":             valueFlag,
			"column":              valueFlag,
			"resolution":          valueFlag,
			"resolution-ref":      valueFlag,
			"reason":              valueFlag,
			"body":                valueFlag,
			"body-file":           valueFlag,
			"summary":             valueFlag,
			"if-board-updated-at": valueFlag,
			"actor-id":            valueFlag,
			"from-file":           valueFlag,
			"dry-run":             boolFlag,
		},
		"cards reopen":              {"card-id": valueFlag},
		"threads message":           {"thread": valueFlag, "thread-id": valueFlag},
		"threads reply":             {"thread": valueFlag, "thread-id": valueFlag},
		"boards workspace":          {"board-id": valueFlag},
		"work create":               {"title": valueFlag, "initiative": boolFlag, "under": valueFlag, "from-file": valueFlag, "handle": valueFlag, "request-key": valueFlag, "after": valueFlag, "top-level": boolFlag},
		"mv":                        {"under": valueFlag, "root": boolFlag, "unsorted": boolFlag, "reason": valueFlag, "after": valueFlag, "request-key": valueFlag},
		"work move":                 {"under": valueFlag, "root": boolFlag, "unsorted": boolFlag, "reason": valueFlag, "after": valueFlag, "request-key": valueFlag},
		"tree":                      {"depth": valueFlag, "include-done": boolFlag},
		"work tree":                 {"depth": valueFlag, "include-done": boolFlag},
		"work ls":                   {"include-done": boolFlag},
		"where":                     {},
		"boards cards create-batch": {"top-level": boolFlag, "under": valueFlag, "board-id": valueFlag, "from-file": valueFlag, "actor-id": valueFlag, "request-key": valueFlag, "if-board-updated-at": valueFlag},
		"boards cards list":         {"board-id": valueFlag},
		"workspace summary":         {"full-id": boolFlag},
		"attention place":           {"for": valueFlag, "to": valueFlag, "reason": valueFlag, "rank": valueFlag, "request-key": valueFlag},
		"attention items":           {"for": valueFlag, "surface": valueFlag, "since": valueFlag, "scope-ref": valueFlag, "cursor": valueFlag},
		"attention feedback list":   {"for": valueFlag, "since": valueFlag, "cursor": valueFlag},
		"attention feedback create": {"signal": valueFlag, "surface": valueFlag, "note": valueFlag, "placed-by": valueFlag, "seconds-visible": valueFlag, "request-key": valueFlag},
		"attention visits":          {"scope-ref": valueFlag},
		"attention changes":         {"scope-ref": valueFlag},
		"attention agent get":       {}, "attention agent set": {},
		"overview":                {},
		"workspace dashboard set": {},
	}
}
