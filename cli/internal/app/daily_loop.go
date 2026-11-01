package app

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
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
	var askFlag, force, again trackedBool
	var body, bodyFile, reason trackedString
	var recommend trackedString
	var situation, impact, why, ifNoAnswer, details trackedString
	var alternatives trackedStrings
	var alts trackedStrings
	fs.Var(&situation, "situation", "Plain-language situation for the ask")
	fs.Var(&impact, "impact", "Who or what is affected")
	fs.Var(&why, "why", "Why the recommendation is best")
	fs.Var(&ifNoAnswer, "if-no-answer", "Default or blocked work and by when")
	fs.Var(&details, "details", "Collapsed technical detail")
	fs.Var(&alternatives, "alternative", "Action=consequence (repeatable)")
	fs.Var(&askFlag, "ask", "Open an operator ask when blocking")
	fs.Var(&recommend, "recommend", "Recommended answer for the optional ask")
	fs.Var(&alts, "alt", "Alternative answer for the optional ask (repeatable)")
	fs.Var(&body, "body", "Markdown body for the optional ask")
	fs.Var(&bodyFile, "body-file", "Read optional ask body from file or stdin (-)")
	fs.Var(&force, "force", "Override ask authoring lint with --reason")
	fs.Var(&again, "again", "File a new ask even though this question is already open or answered on the card")
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
	if verb != "block" && (situation.set || impact.set || why.set || ifNoAnswer.set || details.set || len(alternatives.values) > 0 || askFlag.set || recommend.set || len(alts.values) > 0 || body.set || bodyFile.set || force.set || reason.set) {
		return nil, errnorm.Usage("invalid_flags", "ask flags belong to work block")
	}
	if verb == "block" && askFlag.value && strings.TrimSpace(recommend.value) == "" {
		return nil, errnorm.Usage("invalid_request", "work block --ask requires --recommend")
	}
	if verb == "block" && !askFlag.value && (situation.set || impact.set || why.set || ifNoAnswer.set || details.set || len(alternatives.values) > 0 || recommend.set || len(alts.values) > 0 || body.set || bodyFile.set || force.set || reason.set) {
		return nil, errnorm.Usage("invalid_flags", "--situation, --impact, --recommend, --why, --alternative, --if-no-answer, --details, --alt, --body, --body-file, --force and --reason require --ask")
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
		if again.value {
			askArgs = append(askArgs, "--again")
		}
		for _, f := range []struct {
			name string
			flag trackedString
		}{{"situation", situation}, {"impact", impact}, {"why", why}, {"if-no-answer", ifNoAnswer}, {"details", details}} {
			if f.flag.set {
				askArgs = append(askArgs, "--"+f.name, f.flag.value)
			}
		}
		for _, alt := range alternatives.values {
			askArgs = append(askArgs, "--alternative", alt)
		}
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
			data := map[string]any{"card_ref": cardRef, "phase": "blocked", "ask_id": commandResultBody(askResult)["ask_id"]}
			for _, key := range []string{"created", "status", "answer"} {
				if value, ok := commandResultBody(askResult)[key]; ok {
					data[key] = value
				}
			}
			result := &commandResult{Data: data, Warnings: askResult.Warnings, NextActions: askResult.NextActions}
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
	var until, timeout, settle trackedString
	var answers, all trackedBool
	fs.Var(&until, "until", "answered or state=<phase>")
	fs.Var(&timeout, "timeout", "Maximum wait duration, default 30m")
	fs.Var(&answers, "answers", "Wait for answers to this session's asks that it has not been handed yet")
	fs.Var(&settle, "settle", "With --answers: how long to keep collecting after an answer while asks stay open, default 20s")
	fs.Var(&all, "all", "With --answers: include asks filed by other sessions of this agent")
	if err := fs.Parse(rest); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	refs := append(leading, fs.Args()...)
	if answers.value {
		if len(refs) != 0 || until.set {
			return nil, errnorm.Usage("invalid_args", "usage: anx await --answers [--timeout <dur>] [--settle <dur>] [--all]")
		}
		opts := answerWaitOptions{timeout: 30 * time.Minute, settle: answerWaitDefaultSettle, all: all.value}
		if timeout.set {
			var err error
			opts.timeout, err = time.ParseDuration(timeout.value)
			if err != nil || opts.timeout <= 0 {
				return nil, errnorm.Usage("invalid_request", "--timeout must be a positive duration")
			}
		}
		if settle.set {
			var err error
			opts.settle, err = time.ParseDuration(settle.value)
			if err != nil || opts.settle < 0 {
				return nil, errnorm.Usage("invalid_request", "--settle must be zero or a positive duration")
			}
		}
		return a.runAwaitAnswers(ctx, cfg, opts)
	}
	if settle.set || all.set {
		return nil, errnorm.Usage("invalid_args", "--settle and --all apply only to anx await --answers")
	}
	if len(refs) != 1 {
		return nil, errnorm.Usage("invalid_args", "usage: anx await <ask-id|card-ref|access-request:<id>> [--until answered|state=<phase>] [--timeout <dur>]")
	}
	target := refs[0]
	if !strings.HasPrefix(target, "card:") && !strings.HasPrefix(target, "access-request:") {
		target = normalizeAskRef(target)
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
