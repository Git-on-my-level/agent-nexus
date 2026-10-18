package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/internal/streaming"
)

const orientPageLimit = 200
const orientReturnLimit = 50

func isDailyWorkVerb(verb string) bool {
	switch verb {
	case "start", "block":
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
		return "", errnorm.WithDetails(errnorm.Usage("no_current_task", "no current card in this workspace; choose a same-workspace card or rerun with --workspace <alias>; run anx orient, then anx work start <card-ref>"), map[string]any{"next_argv": []string{"anx", "orient"}})
	}
	return ref, nil
}

func (a *App) runDailyWork(ctx context.Context, verb string, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("work " + verb)
	var askFlag, force trackedBool
	var body, bodyFile, reason trackedString
	var recommend trackedString
	var alts trackedStrings
	fs.Var(&askFlag, "ask", "Open an operator ask when blocking")
	fs.Var(&recommend, "recommend", "Recommended answer for the optional ask")
	fs.Var(&alts, "alt", "Alternative answer for the optional ask (repeatable)")
	fs.Var(&body, "body", "Markdown body for the optional ask")
	fs.Var(&bodyFile, "body-file", "Read optional ask body from file or stdin (-)")
	fs.Var(&force, "force", "Override ask authoring lint with --reason")
	fs.Var(&reason, "reason", "Recorded ask authoring override reason")
	leading, rest := splitLeadingPositionals(args)
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	var text, cardRef string
	if verb == "block" {
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
	if verb != "block" && (askFlag.set || recommend.set || len(alts.values) > 0 || body.set || bodyFile.set || force.set || reason.set) {
		return nil, errnorm.Usage("invalid_flags", "ask flags belong to work block")
	}
	if verb == "block" && askFlag.value && strings.TrimSpace(recommend.value) == "" {
		return nil, errnorm.Usage("invalid_request", "work block --ask requires --recommend")
	}
	if verb == "block" && !askFlag.value && (recommend.set || len(alts.values) > 0 || body.set || bodyFile.set || force.set || reason.set) {
		return nil, errnorm.Usage("invalid_flags", "--recommend, --alt, --body, --body-file, --force and --reason require --ask")
	}
	card, err := a.fetchCardBody(ctx, cfg, cardRef)
	if err != nil {
		return nil, err
	}
	cardRef = firstNonEmpty(anyString(card["ref"]), cardRef)
	if !strings.HasPrefix(cardRef, "card:") {
		cardRef = "card:" + cardRef
	}
	var askArgs []string
	if askFlag.value {
		askBody := body.value
		if strings.TrimSpace(bodyFile.value) != "" {
			content, err := a.readBodyInput(bodyFile.value)
			if err != nil {
				return nil, err
			}
			askBody = string(content)
		}
		askArgs = []string{text, "--subject-ref", cardRef, "--recommend", recommend.value, "--body", askBody}
		for _, alt := range alts.values {
			askArgs = append(askArgs, "--alt", alt)
		}
		if force.value {
			askArgs = append(askArgs, "--force")
		}
		if reason.set {
			askArgs = append(askArgs, "--reason", reason.value)
		}
		// Validate the complete ask before moving the card or posting its blocker.
		// Read body files once so stdin and file contents match the validated ask.
		previewArgs := append(append([]string(nil), askArgs...), "--dry-run")
		if _, err := a.runHumanAttentionCommand(ctx, "ask", previewArgs, cfg); err != nil {
			return nil, err
		}
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
			askResult, err := a.runHumanAttentionCommand(ctx, "ask", askArgs, cfg)
			if err != nil {
				return nil, err
			}
			result := &commandResult{Data: map[string]any{"card_ref": cardRef, "phase": "blocked", "ask_id": commandResultBody(askResult)["ask_id"]}, Warnings: askResult.Warnings}
			return result, nil
		}
	}
	result := &commandResult{Data: map[string]any{"card_ref": cardRef, "phase": map[string]string{"start": "in_progress", "block": "blocked"}[verb], "note": text}}
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
	var answers trackedBool
	fs.Var(&until, "until", "answered or state=<phase>")
	fs.Var(&timeout, "timeout", "Maximum wait duration, default 30m")
	fs.Var(&answers, "answers", "Wait for a debounced batch of answers to my asks")
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	refs := append(leading, fs.Args()...)
	if answers.value {
		if len(refs) != 0 || until.set {
			return nil, errnorm.Usage("invalid_args", "usage: anx await --answers [--timeout <dur>]")
		}
		wait := 30 * time.Minute
		if timeout.set {
			var err error
			wait, err = time.ParseDuration(timeout.value)
			if err != nil || wait <= 0 {
				return nil, errnorm.Usage("invalid_request", "--timeout must be a positive duration")
			}
		}
		return a.runAwaitAnswers(ctx, cfg, wait)
	}
	if len(refs) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx await <ask-id|card-ref|access-request:<id>> [--until answered|state=<phase>] [--timeout <dur>]")
	}
	target := refs[0]
	if !strings.HasPrefix(target, "card:") && !strings.HasPrefix(target, "event:") && !strings.HasPrefix(target, "access-request:") {
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
	if !targetIsCard {
		return a.runAwaitAsk(ctx, cfg, target, wait)
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

func (a *App) runAwaitAnswers(ctx context.Context, cfg config.Resolved, timeout time.Duration) (*commandResult, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastUnmatched []string
	readBatch := func() (*commandResult, bool, error) {
		notifications, err := a.invokeRawJSON(waitCtx, cfg, "await answer notifications", "GET", "/agent-notifications?status=unread", nil)
		if err != nil {
			return nil, false, err
		}
		responseIDs := map[string]bool{}
		for _, raw := range asSlice(commandResultBody(notifications)["items"]) {
			item := asMap(raw)
			triggerID := strings.TrimSpace(anyString(item["trigger_event_id"]))
			if triggerID != "" {
				responseIDs[triggerID] = true
			}
			for _, ref := range stringList(item["related_refs"]) {
				if strings.HasPrefix(ref, "event:") {
					responseIDs[strings.TrimPrefix(ref, "event:")] = true
				}
			}
		}
		if len(responseIDs) == 0 {
			return nil, false, nil
		}
		// Sweep newest-first for the woken answers. The scan stops as soon as
		// every notified answer is matched, or after maxOwnAsksLookupPages
		// pages, so a wake behind newer activity is still found without
		// looping over the whole ask history.
		_, actorID, err := a.resolveOwnAsksActor(waitCtx, cfg)
		if err != nil {
			return nil, false, err
		}
		batch := make([]any, 0, len(responseIDs))
		cursor := ""
		for page := 0; page < maxOwnAsksLookupPages; page++ {
			items, next, pageErr := a.fetchOwnAsksPageForActor(waitCtx, cfg, actorID, cursor, 200)
			if pageErr != nil {
				return nil, false, pageErr
			}
			for _, raw := range items {
				item := asMap(raw)
				answerID := strings.TrimSpace(anyString(asMap(item["answer"])["response_event_id"]))
				if answerID == "" || !responseIDs[answerID] {
					continue
				}
				batch = append(batch, item)
				delete(responseIDs, answerID)
			}
			if len(responseIDs) == 0 || next == "" {
				break
			}
			cursor = next
		}
		// Whatever is left after the bounded window is preserved, never
		// silently dropped: notified answers older than the newest
		// maxOwnAsksLookupPages*200 asks stay listed as unmatched.
		unmatched := make([]string, 0, len(responseIDs))
		for answerID := range responseIDs {
			unmatched = append(unmatched, answerID)
		}
		sort.Strings(unmatched)
		lastUnmatched = unmatched
		if len(batch) == 0 {
			return nil, false, nil
		}
		data := map[string]any{"answers": batch, "count": len(batch)}
		result := &commandResult{Data: data}
		if len(unmatched) > 0 {
			data["incomplete"] = true
			data["unmatched_notification_ids"] = unmatched
			result.Warnings = append(result.Warnings, output.Warning{Code: "ask_delivery", Message: fmt.Sprintf("%d notified answer(s) were not found among the newest %d asks; they may answer older asks. Continue with: anx inbox list --status answered --unread --limit 200, then page with --cursor", len(unmatched), maxOwnAsksLookupPages*200)})
		}
		return result, true, nil
	}
	if result, ready, err := readBatch(); err != nil || ready {
		return result, err
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			details := map[string]any{"target": "answers", "timeout": timeout.String()}
			if len(lastUnmatched) > 0 {
				details["unmatched_notification_ids"] = lastUnmatched
				details["continuation"] = fmt.Sprintf("%d notified answer(s) were never found among the newest %d asks; continue with: anx inbox list --status answered --unread --limit 200, then page with --cursor", len(lastUnmatched), maxOwnAsksLookupPages*200)
			}
			return nil, errnorm.WithDetails(errnorm.New(errnorm.KindNetwork, "timeout", "await timed out waiting for answer batch"), details)
		case <-ticker.C:
			result, ready, err := readBatch()
			if err != nil {
				return nil, err
			}
			if ready {
				return result, nil
			}
		}
	}
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
