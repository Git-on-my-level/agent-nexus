package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/streaming"
)

const orientPageLimit = 200
const orientReturnLimit = 50

func isDailyWorkVerb(verb string) bool {
	switch verb {
	case "start", "note", "block", "done":
		return true
	}
	return false
}

// dailyAgent resolves the derived agent through the enrolled host.
func (a *App) dailyAgent(ctx context.Context, cfg config.Resolved) (map[string]any, map[string]any, error) {
	if cfg.AgentID == "" {
		var err error
		cfg, err = a.resolveHostAgent(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
	}
	id := strings.TrimSpace(cfg.AgentID)
	if id == "" {
		return nil, nil, errnorm.Usage("identity_unresolved", "select a derived agent with --as <name>")
	}
	result, err := a.invokeRawJSON(ctx, cfg, "agents get", "GET", "/agents/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, nil, err
	}
	body := commandResultBody(result)
	return asMap(body["agent"]), body, nil
}

func (a *App) currentCardRef(ctx context.Context, cfg config.Resolved) (string, error) {
	agent, _, err := a.dailyAgent(ctx, cfg)
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(anyString(agent["current_card_ref"]))
	if ref == "" {
		return "", errnorm.WithDetails(errnorm.Usage("no_current_task", "no current card; run anx orient, then anx work start <card-ref>"), map[string]any{"next_argv": []string{"anx", "orient"}})
	}
	return ref, nil
}

func (a *App) runOrient(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("orient")
	var staleHours trackedInt
	fs.Var(&staleHours, "stale-hours", "Hours without a progress note before in-progress work is stale (default 24)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return nil, errnorm.Usage("invalid_args", "orient takes no positional arguments")
	}
	hours := 24
	if staleHours.set {
		hours = staleHours.value
	}
	if hours < 1 || hours > 720 {
		return nil, errnorm.Usage("invalid_request", "--stale-hours must be 1..720")
	}
	agent, detail, err := a.dailyAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	actorRef := "actor:" + strings.TrimPrefix(anyString(agent["actor_id"]), "actor:")
	workResult, err := a.invokeRawJSON(ctx, cfg, "orient work", "GET", "/work?limit=200", nil)
	if err != nil {
		return nil, err
	}
	workBody := commandResultBody(workResult)
	matched := 0
	workByPhase := map[string]any{}
	stale := []any{}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)
	notes := asSlice(detail["recent_notes"])
	for _, raw := range asSlice(workBody["work"]) {
		row := asMap(raw)
		assigned := false
		for _, ref := range asSlice(row["assignee_refs"]) {
			if anyString(ref) == actorRef {
				assigned = true
				break
			}
		}
		if !assigned {
			continue
		}
		matched++
		if matched > orientReturnLimit {
			continue
		}
		phase := firstNonEmpty(anyString(row["phase"]), "unknown")
		group := asSlice(workByPhase[phase])
		group = append(group, row)
		workByPhase[phase] = group
		if phase == "in_progress" {
			latest := time.Time{}
			for _, n := range notes {
				note := asMap(n)
				if anyString(note["card_ref"]) != anyString(row["ref"]) {
					continue
				}
				at, parseErr := time.Parse(time.RFC3339Nano, anyString(note["at"]))
				if parseErr == nil && at.After(latest) {
					latest = at
				}
			}
			if latest.IsZero() || latest.Before(cutoff) {
				stale = append(stale, row)
			}
		}
	}
	requests, err := a.invokeRawJSON(ctx, cfg, "orient asks", "GET", "/events?type=human_attention_requested&limit=100", nil)
	if err != nil {
		return nil, err
	}
	responses, err := a.invokeRawJSON(ctx, cfg, "orient answers", "GET", "/events?type=human_attention_responded&limit=100", nil)
	if err != nil {
		return nil, err
	}
	ownRequests := []any{}
	answerByRequest := map[string]any{}
	for _, raw := range asSlice(commandResultBody(responses)["events"]) {
		event := asMap(raw)
		payload := asMap(event["payload"])
		if anyString(payload["requester_actor_id"]) != anyString(agent["actor_id"]) {
			continue
		}
		ref := responseAskID(payload)
		if ref != "" {
			answerByRequest[ref] = map[string]any{"text": payload["response_text"], "outcome": payload["outcome"], "responder": payload["responding_actor_id"], "at": event["ts"], "response_event_id": event["id"]}
		}
	}
	askMatched := 0
	for _, raw := range asSlice(commandResultBody(requests)["events"]) {
		event := asMap(raw)
		payload := asMap(event["payload"])
		if anyString(payload["requester_actor_id"]) != anyString(agent["actor_id"]) {
			continue
		}
		askMatched++
		if len(ownRequests) >= orientReturnLimit {
			continue
		}
		ref := "event:" + anyString(event["id"])
		ownRequests = append(ownRequests, map[string]any{"ask_id": ref, "title": payload["title"], "subject_ref": payload["subject_ref"], "answer": answerByRequest[ref]})
	}
	notifications, err := a.invokeRawJSON(ctx, cfg, "orient notifications", "GET", "/agent-notifications?status=unread", nil)
	if err != nil {
		return nil, err
	}
	notificationBody := commandResultBody(notifications)
	allNotifications := asSlice(notificationBody["items"])
	returnedNotifications := allNotifications
	if len(returnedNotifications) > orientReturnLimit {
		returnedNotifications = returnedNotifications[:orientReturnLimit]
	}
	unreadEvents := map[string]bool{}
	for _, raw := range allNotifications {
		item := asMap(raw)
		unreadEvents[anyString(item["trigger_event_id"])] = true
	}
	for _, raw := range ownRequests {
		ask := asMap(raw)
		answer := asMap(ask["answer"])
		id := anyString(answer["response_event_id"])
		ask["answer_unread"] = id != "" && unreadEvents[id]
	}
	sort.SliceStable(ownRequests, func(i, j int) bool {
		return asMap(ownRequests[i])["answer_unread"] == true && asMap(ownRequests[j])["answer_unread"] != true
	})
	next := []any{}
	if ref := anyString(agent["current_card_ref"]); ref != "" {
		next = append(next, []string{"anx", "cards", "get", ref})
	}
	allOpenAsks := asSlice(detail["open_asks"])
	returnedOpenAsks := allOpenAsks
	if len(returnedOpenAsks) > orientReturnLimit {
		returnedOpenAsks = returnedOpenAsks[:orientReturnLimit]
	}
	for _, raw := range returnedOpenAsks {
		if len(next) >= 10 {
			break
		}
		ask := asMap(raw)
		if ref := inboxAskID(ask); ref != "" {
			next = append(next, []string{"anx", "await", ref})
		}
	}
	if len(next) == 0 {
		for _, phase := range []string{"in_progress", "ready", "backlog"} {
			items := asSlice(workByPhase[phase])
			if len(items) > 0 {
				if ref := anyString(asMap(items[0])["ref"]); ref != "" {
					next = append(next, []string{"anx", "work", "start", ref})
					break
				}
			}
		}
	}
	if len(next) == 0 {
		next = append(next, []string{"anx", "work", "list"})
	}
	resolution := cfg.IdentitySource
	if resolution == "" {
		_, resolution, _ = a.identityName(cfg)
	}
	result := map[string]any{
		"me":               map[string]any{"agent": agent["id"], "handle": agent["handle"], "host": agent["host_slug"], "current_card_ref": agent["current_card_ref"], "identity_resolved_by": resolution},
		"my_work_by_phase": workByPhase, "my_work_matched": matched, "my_work_returned": min(matched, orientReturnLimit),
		"work_page_limit": orientPageLimit, "work_has_more": anyString(workBody["next_cursor"]) != "", "work_next_cursor": workBody["next_cursor"],
		"my_asks_and_answers": ownRequests, "my_asks_matched": askMatched, "my_asks_returned": len(ownRequests), "ask_page_limit": 100,
		"open_asks": returnedOpenAsks, "open_asks_matched": len(allOpenAsks), "open_asks_returned": len(returnedOpenAsks),
		"mentions_notifications": map[string]any{"items": returnedNotifications, "matched": len(allNotifications), "returned": len(returnedNotifications)},
		"stale_items":            stale, "stale_hours": hours, "next": next,
	}
	return &commandResult{Data: result}, nil
}

func (a *App) runDailyWork(ctx context.Context, verb string, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("work " + verb)
	var askFlag trackedBool
	var recommend trackedString
	var alts, evidence trackedStrings
	fs.Var(&askFlag, "ask", "Open an operator ask when blocking")
	fs.Var(&recommend, "recommend", "Recommended answer for the optional ask")
	fs.Var(&alts, "alt", "Alternative answer for the optional ask (repeatable)")
	fs.Var(&evidence, "evidence", "Evidence URL or typed ref (repeatable)")
	leading, rest := splitLeadingPositionals(args)
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	var text, cardRef string
	if verb == "note" || verb == "block" {
		if len(positionals) == 0 {
			return nil, errnorm.Usage("invalid_request", "progress text is required")
		}
		text = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
		if text == "" {
			return nil, errnorm.Usage("invalid_request", "progress text is required")
		}
	}
	if len(positionals) > 1 {
		return nil, errnorm.Usage("invalid_args", "pass at most one card ref")
	}
	if len(positionals) == 1 {
		cardRef = positionals[0]
	} else {
		var err error
		cardRef, err = a.currentCardRef(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	if verb != "block" && (askFlag.set || recommend.set || len(alts.values) > 0) {
		return nil, errnorm.Usage("invalid_flags", "--ask, --recommend and --alt belong to work block")
	}
	if verb != "done" && len(evidence.values) > 0 {
		return nil, errnorm.Usage("invalid_flags", "--evidence belongs to work done")
	}
	if verb == "done" && len(evidence.values) == 0 {
		return nil, errnorm.Usage("invalid_request", "work done requires --evidence <url|ref>")
	}
	if verb == "block" && askFlag.value && strings.TrimSpace(recommend.value) == "" {
		return nil, errnorm.Usage("invalid_request", "work block --ask requires --recommend")
	}
	if verb == "block" && !askFlag.value && (recommend.set || len(alts.values) > 0) {
		return nil, errnorm.Usage("invalid_flags", "--recommend and --alt require --ask")
	}
	card, err := a.fetchCardBody(ctx, cfg, cardRef)
	if err != nil {
		return nil, err
	}
	cardRef = firstNonEmpty(anyString(card["ref"]), cardRef)
	if !strings.HasPrefix(cardRef, "card:") {
		cardRef = "card:" + cardRef
	}
	switch verb {
	case "start":
		agent, _, err := a.dailyAgent(ctx, cfg)
		if err != nil {
			return nil, err
		}
		self := "actor:" + strings.TrimPrefix(anyString(agent["actor_id"]), "actor:")
		assignees := stringList(card["assignee_refs"])
		found := false
		for _, ref := range assignees {
			if ref == self {
				found = true
				break
			}
		}
		if !found {
			assignees = append(assignees, self)
			body := map[string]any{"patch": map[string]any{"assignee_refs": assignees}, "if_updated_at": card["updated_at"]}
			if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
				return nil, err
			}
			if _, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards assign", "cards.patch", "card_id", cardRef, cardIDLookupSpec, nil, body); err != nil {
				return nil, err
			}
		}
		if anyString(card["column_key"]) != "in_progress" {
			if err := a.dailyMove(ctx, cfg, cardRef, "in_progress"); err != nil {
				return nil, err
			}
		}
		if _, err := a.invokeRawJSON(ctx, cfg, "work presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": cardRef}); err != nil {
			return nil, err
		}
	case "note":
		if _, _, err := a.runCardsCommand(ctx, []string{"message", cardRef, "--body", text}, cfg); err != nil {
			return nil, err
		}
		if _, err := a.invokeRawJSON(ctx, cfg, "work presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": cardRef, "note": truncateRunes(text, 500)}); err != nil {
			return nil, err
		}
	case "block":
		if err := a.dailyMove(ctx, cfg, cardRef, "blocked"); err != nil {
			return nil, err
		}
		if _, _, err := a.runCardsCommand(ctx, []string{"message", cardRef, "--body", text}, cfg); err != nil {
			return nil, err
		}
		if _, err := a.invokeRawJSON(ctx, cfg, "work presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": cardRef, "note": truncateRunes(text, 500)}); err != nil {
			return nil, err
		}
		if askFlag.value {
			askArgs := []string{text, "--subject-ref", cardRef, "--recommend", recommend.value}
			for _, alt := range alts.values {
				askArgs = append(askArgs, "--alt", alt)
			}
			askResult, err := a.runHumanAttentionCommand(ctx, "ask", askArgs, cfg)
			if err != nil {
				return nil, err
			}
			result := &commandResult{Data: map[string]any{"card_ref": cardRef, "phase": "blocked", "ask_id": commandResultBody(askResult)["ask_id"]}}
			return result, nil
		}
	case "done":
		resolveArgs := []string{"resolve", cardRef}
		var urls []string
		for _, e := range evidence.values {
			if strings.HasPrefix(e, "event:") || strings.HasPrefix(e, "artifact:") {
				resolveArgs = append(resolveArgs, "--resolution-ref", e)
			} else {
				u, parseErr := url.Parse(e)
				if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return nil, errnorm.Usage("invalid_request", "--evidence must be an http(s) URL or event:/artifact: ref")
				}
				urls = append(urls, e)
			}
		}
		if len(urls) > 0 {
			resolveArgs = append(resolveArgs, "--body", strings.Join(urls, "\n"))
		}
		if _, _, err := a.runCardsCommand(ctx, resolveArgs, cfg); err != nil {
			return nil, err
		}
		if _, err := a.invokeRawJSON(ctx, cfg, "work presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": nil, "note": nil}); err != nil {
			return nil, err
		}
	}
	result := &commandResult{Data: map[string]any{"card_ref": cardRef, "phase": map[string]string{"start": "in_progress", "note": anyString(card["column_key"]), "block": "blocked", "done": "done"}[verb], "note": text}}
	return result, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func (a *App) dailyMove(ctx context.Context, cfg config.Resolved, ref, phase string) error {
	body := map[string]any{"column_key": phase}
	if err := a.ensureCardMoveConcurrency(ctx, cfg, ref, body); err != nil {
		return err
	}
	_, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards move", "cards.move", "card_id", ref, cardIDLookupSpec, nil, body)
	return err
}

func (a *App) runAwait(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leading, rest := splitLeadingPositionals(args)
	fs := newSilentFlagSet("await")
	var until, timeout trackedString
	fs.Var(&until, "until", "answered or state=<phase>")
	fs.Var(&timeout, "timeout", "Maximum wait duration, default 30m")
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	refs := append(leading, fs.Args()...)
	if len(refs) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx await <ask-id|card-ref> [--until answered|state=<phase>] [--timeout <dur>]")
	}
	target := refs[0]
	if !strings.HasPrefix(target, "card:") && !strings.HasPrefix(target, "event:") {
		target = "event:" + target
	}
	wait := 30 * time.Minute
	if timeout.set {
		var err error
		wait, err = time.ParseDuration(timeout.value)
		if err != nil || wait <= 0 {
			return nil, errnorm.Usage("invalid_request", "--timeout must be a positive duration")
		}
	}
	targetIsCard := strings.HasPrefix(target, "card:")
	condition := strings.TrimSpace(until.value)
	if condition == "" {
		if targetIsCard {
			condition = "state=done"
		} else {
			condition = "answered"
		}
	}
	if targetIsCard && !strings.HasPrefix(condition, "state=") || !targetIsCard && condition != "answered" {
		return nil, errnorm.Usage("invalid_request", "ask targets require answered; card targets require state=<phase>")
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	threadID := ""
	if !targetIsCard {
		event, err := a.dailyAskEvent(waitCtx, cfg, target)
		if err != nil {
			return nil, err
		}
		threadID = anyString(event["thread_id"])
		if threadID == "" {
			return nil, errnorm.Usage("invalid_request", "ask has no thread")
		}
	}
	check := func() (map[string]any, bool, error) {
		if targetIsCard {
			card, err := a.fetchCardBody(waitCtx, cfg, target)
			if err != nil {
				return nil, false, err
			}
			phase := anyString(card["column_key"])
			if phase == strings.TrimPrefix(condition, "state=") {
				return map[string]any{"target": target, "state": phase}, true, nil
			}
			return nil, false, nil
		}
		path := "/events?thread_id=" + url.QueryEscape(threadID) + "&type=human_attention_responded&limit=200"
		result, err := a.invokeRawJSON(waitCtx, cfg, "await answers", "GET", path, nil)
		if err != nil {
			return nil, false, err
		}
		for _, raw := range asSlice(commandResultBody(result)["events"]) {
			ev := asMap(raw)
			payload := asMap(ev["payload"])
			if responseAskID(payload) != target {
				continue
			}
			result, responseErr := awaitResponseResult(target, ev)
			return result, responseErr == nil, responseErr
		}
		return nil, false, nil
	}
	if result, done, err := check(); err != nil {
		return nil, err
	} else if done {
		return &commandResult{Data: result}, nil
	}
	authCfg := cfg
	cursor := ""
	backoff := 250 * time.Millisecond
	for waitCtx.Err() == nil {
		var err error
		authCfg, err = a.cfgWithResolvedAuthToken(waitCtx, authCfg)
		if err != nil {
			return nil, err
		}
		authCfg.Timeout = 0
		client, err := httpclient.New(authCfg)
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "cannot initialize stream client", err)
		}
		path := streamPathForCommand("events.stream", nil, cursor)
		if threadID != "" {
			path = streamPathForCommand("events.stream", []queryParam{{name: "thread_id", values: []string{threadID}}}, cursor)
		}
		headers := map[string]string{"Accept": "text/event-stream"}
		if cursor != "" {
			headers["Last-Event-ID"] = cursor
		}
		resp, openErr := client.OpenStream(waitCtx, httpclient.RawRequest{Method: http.MethodGet, Path: path, Headers: headers})
		if openErr == nil {
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return nil, errnorm.FromHTTPFailure(resp.StatusCode, body)
			}
			backoff = 250 * time.Millisecond
			reader := bufio.NewReader(resp.Body)
			for waitCtx.Err() == nil {
				event, readErr := streaming.ReadEvent(reader)
				if readErr != nil {
					break
				}
				if event.ID != "" {
					cursor = event.ID
				}
				if event.Type == "error" {
					break
				}
				var data map[string]any
				if json.Unmarshal([]byte(event.Data), &data) != nil {
					continue
				}
				if targetIsCard {
					result, done, checkErr := check()
					if checkErr != nil {
						resp.Body.Close()
						return nil, checkErr
					}
					if done {
						resp.Body.Close()
						return &commandResult{Data: result}, nil
					}
				} else {
					ev := asMap(data["event"])
					payload := asMap(ev["payload"])
					if anyString(ev["type"]) != "human_attention_responded" || responseAskID(payload) != target {
						continue
					}
					resp.Body.Close()
					data, responseErr := awaitResponseResult(target, ev)
					if responseErr != nil {
						return nil, responseErr
					}
					result := &commandResult{Data: data}
					return result, nil
				}
			}
			resp.Body.Close()
		}
		select {
		case <-waitCtx.Done():
			break
		case <-time.After(backoff):
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	return nil, errnorm.WithDetails(errnorm.New(errnorm.KindNetwork, "timeout", "await timed out"), map[string]any{"target": target, "timeout": wait.String()})
}

func awaitResponseResult(target string, event map[string]any) (map[string]any, error) {
	payload := asMap(event["payload"])
	outcome := anyString(payload["outcome"])
	result := map[string]any{
		"ask_id": target, "outcome": outcome, "answer": payload["response_text"],
		"responder": payload["responding_actor_id"], "subject_ref": payload["subject_ref"],
		"response_event_ref": "event:" + anyString(event["id"]),
	}
	switch outcome {
	case "answered", "approved", "acknowledged":
		return result, nil
	case "rejected":
		return nil, errnorm.WithDetails(errnorm.New(errnorm.KindRemote, "rejected", "operator rejected the request"), result)
	default:
		return nil, errnorm.WithDetails(errnorm.New(errnorm.KindRemote, "invalid_response_outcome", "response event has no valid outcome"), result)
	}
}

func (a *App) dailyAskEvent(ctx context.Context, cfg config.Resolved, ref string) (map[string]any, error) {
	id := strings.TrimPrefix(ref, "event:")
	if id == "" {
		return nil, errnorm.Usage("invalid_request", "ask id is required")
	}
	result, err := a.invokeRawJSON(ctx, cfg, "await ask", "GET", "/events/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	event := asMap(commandResultBody(result)["event"])
	if anyString(event["type"]) != "human_attention_requested" {
		return nil, errnorm.Usage("invalid_request", "target is not an ask event")
	}
	return event, nil
}

// Core may put the request's public event handle in request_event_ref. That
// handle need not be unique; the inbox id always ends with the source event id.
func responseAskID(payload map[string]any) string {
	if id := inboxAskID(payload); id != "" {
		return id
	}
	return anyString(payload["request_event_ref"])
}

func inboxAskID(item map[string]any) string {
	if source := anyString(item["source_event_id"]); source != "" {
		return "event:" + source
	}
	if id := firstNonEmpty(anyString(item["inbox_item_id"]), anyString(item["id"])); strings.HasPrefix(id, "inbox:") {
		parts := strings.Split(id, ":")
		if source := parts[len(parts)-1]; source != "" {
			return "event:" + source
		}
	}
	return ""
}
