package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type messageTarget struct {
	SubjectKind string
	SubjectID   string
	SubjectRef  string
	Title       string
	ThreadID    string
	BoardRef    string
	MessageKind string
}

func (a *App) parseTopicMessageInput(ctx context.Context, args []string, cfg config.Resolved, commandName string, replyToEventID string) (map[string]any, messageTarget, bool, error) {
	leadingTopicID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var topicIDFlag, threadIDFlag, bodyFlag, bodyFileFlag, summaryFlag, actorIDFlag trackedString
	var refFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&topicIDFlag, "topic", "Topic id to message")
	fs.Var(&topicIDFlag, "topic-id", "Topic id to message")
	fs.Var(&threadIDFlag, "thread", "Backing thread id to message")
	fs.Var(&threadIDFlag, "thread-id", "Backing thread id to message")
	fs.Var(&bodyFlag, "body", "Message body text")
	fs.Var(&bodyFileFlag, "body-file", "Load message body text from a local file")
	fs.Var(&summaryFlag, "summary", "Optional short event summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id; defaults from the active profile")
	fs.Var(&refFlags, "ref", "Additional typed ref to attach to the message, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	topicID := firstNonEmpty(strings.TrimSpace(topicIDFlag.value), leadingTopicID)
	if topicID == "" && len(positionals) > 0 {
		topicID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	threadID := strings.TrimSpace(threadIDFlag.value)
	if topicID != "" && threadID != "" {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("provide only one of --topic or --thread for `anx %s`", commandName))
	}
	if topicID == "" && threadID == "" {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("topic id is required for `anx %s`; pass --topic or --thread", commandName))
	}
	if topicID != "" {
		if err := validateID(topicID, "topic id"); err != nil {
			return nil, messageTarget{}, false, err
		}
	} else if err := validateID(threadID, "thread id"); err != nil {
		return nil, messageTarget{}, false, err
	}
	message, err := a.readMessageText(bodyFlag.value, bodyFileFlag.value, commandName)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	var target messageTarget
	if threadID != "" {
		target = threadMessageTarget(threadID)
	} else {
		topic, err := a.fetchTopicBody(ctx, cfg, topicID)
		if err != nil {
			return nil, messageTarget{}, false, err
		}
		target, err = topicMessageTarget(topic, topicID)
		if err != nil {
			return nil, messageTarget{}, false, err
		}
	}
	body, err := buildMessagePostedBody(cfg, actorIDFlag.value, target, message, summaryFlag.value, refFlags.values, replyToEventID)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	return body, target, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseTopicReplyInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]any, messageTarget, bool, error) {
	replyTo, filtered, err := extractReplyTarget(args, commandName)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	if err := validateID(replyTo, "message id"); err != nil {
		return nil, messageTarget{}, false, err
	}
	body, target, dryRun, err := a.parseTopicMessageInput(ctx, filtered, cfg, commandName, "")
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	resolvedReplyTo, err := a.resolveMessageEventOnThread(ctx, cfg, target.ThreadID, replyTo)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	applyReplyToMessageBody(body, resolvedReplyTo)
	return body, target, dryRun, nil
}

func (a *App) parseDocMessageInput(ctx context.Context, args []string, cfg config.Resolved, commandName string, replyToEventID string) (map[string]any, messageTarget, bool, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var documentIDFlag, bodyFlag, bodyFileFlag, summaryFlag, actorIDFlag trackedString
	var refFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&documentIDFlag, "document", "Document id to message")
	fs.Var(&documentIDFlag, "document-id", "Document id to message")
	fs.Var(&bodyFlag, "body", "Message body text")
	fs.Var(&bodyFileFlag, "body-file", "Load message body text from a local file")
	fs.Var(&summaryFlag, "summary", "Optional short event summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id; defaults from the active profile")
	fs.Var(&refFlags, "ref", "Additional typed ref to attach to the message, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, messageTarget{}, false, err
	}
	message, err := a.readMessageText(bodyFlag.value, bodyFileFlag.value, commandName)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	document, err := a.fetchDocumentBody(ctx, cfg, documentID)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	target, err := documentMessageTarget(document, documentID)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	body, err := buildMessagePostedBody(cfg, actorIDFlag.value, target, message, summaryFlag.value, refFlags.values, replyToEventID)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	return body, target, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseDocReplyInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]any, messageTarget, bool, error) {
	replyTo, filtered, err := extractReplyTarget(args, commandName)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	if err := validateID(replyTo, "message id"); err != nil {
		return nil, messageTarget{}, false, err
	}
	body, target, dryRun, err := a.parseDocMessageInput(ctx, filtered, cfg, commandName, "")
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	resolvedReplyTo, err := a.resolveMessageEventOnThread(ctx, cfg, target.ThreadID, replyTo)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	applyReplyToMessageBody(body, resolvedReplyTo)
	return body, target, dryRun, nil
}

func (a *App) parseCardMessageInput(ctx context.Context, args []string, cfg config.Resolved, commandName string, replyToEventID string) (map[string]any, map[string]any, bool, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, bodyFlag, bodyFileFlag, summaryFlag, actorIDFlag trackedString
	var refFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&cardIDFlag, "card", "Card id to message")
	fs.Var(&cardIDFlag, "card-id", "Card id to message")
	fs.Var(&bodyFlag, "body", "Message body text")
	fs.Var(&bodyFileFlag, "body-file", "Load message body text from a local file")
	fs.Var(&summaryFlag, "summary", "Optional short event summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id; defaults from the active profile")
	fs.Var(&refFlags, "ref", "Additional typed ref to attach to the message, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := firstNonEmpty(strings.TrimSpace(cardIDFlag.value), leadingCardID)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return nil, nil, false, err
	}
	message, err := a.readMessageText(bodyFlag.value, bodyFileFlag.value, commandName)
	if err != nil {
		return nil, nil, false, err
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return nil, nil, false, err
	}
	target, err := cardMessageTarget(card, cardID)
	if err != nil {
		return nil, nil, false, err
	}
	body, err := buildMessagePostedBody(cfg, actorIDFlag.value, target, message, summaryFlag.value, refFlags.values, replyToEventID)
	if err != nil {
		return nil, nil, false, err
	}
	return body, card, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseCardReplyInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]any, map[string]any, bool, error) {
	replyTo, filtered, err := extractReplyTarget(args, commandName)
	if err != nil {
		return nil, nil, false, err
	}
	if err := validateID(replyTo, "message id"); err != nil {
		return nil, nil, false, err
	}
	body, card, dryRun, err := a.parseCardMessageInput(ctx, filtered, cfg, commandName, "")
	if err != nil {
		return nil, nil, false, err
	}
	target, err := cardMessageTarget(card, strings.TrimSpace(anyString(card["id"])))
	if err != nil {
		return nil, nil, false, err
	}
	resolvedReplyTo, err := a.resolveMessageEventOnThread(ctx, cfg, target.ThreadID, replyTo)
	if err != nil {
		return nil, nil, false, err
	}
	applyReplyToMessageBody(body, resolvedReplyTo)
	return body, card, dryRun, nil
}

func (a *App) parseThreadMessageInput(ctx context.Context, args []string, cfg config.Resolved, commandName string, replyToEventID string) (map[string]any, messageTarget, bool, error) {
	leadingThreadID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var threadIDFlag, bodyFlag, bodyFileFlag, summaryFlag, actorIDFlag trackedString
	var refFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&threadIDFlag, "thread", "Thread id to message")
	fs.Var(&threadIDFlag, "thread-id", "Thread id to message")
	fs.Var(&bodyFlag, "body", "Message body text")
	fs.Var(&bodyFileFlag, "body-file", "Load message body text from a local file")
	fs.Var(&summaryFlag, "summary", "Optional short event summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id; defaults from the active profile")
	fs.Var(&refFlags, "ref", "Additional typed ref to attach to the message, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	threadID := firstNonEmpty(strings.TrimSpace(threadIDFlag.value), leadingThreadID)
	if threadID == "" && len(positionals) > 0 {
		threadID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, messageTarget{}, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(threadID, "thread id"); err != nil {
		return nil, messageTarget{}, false, err
	}
	message, err := a.readMessageText(bodyFlag.value, bodyFileFlag.value, commandName)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	resolvedThreadID, err := a.resolveMaybeThreadID(ctx, cfg, threadID)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	target := messageTarget{
		SubjectKind: "thread",
		SubjectID:   resolvedThreadID,
		SubjectRef:  "thread:" + resolvedThreadID,
		ThreadID:    resolvedThreadID,
	}
	body, err := buildMessagePostedBody(cfg, actorIDFlag.value, target, message, summaryFlag.value, refFlags.values, replyToEventID)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	return body, target, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseThreadReplyInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]any, messageTarget, bool, error) {
	replyTo, filtered, err := extractReplyTarget(args, commandName)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	if err := validateID(replyTo, "message id"); err != nil {
		return nil, messageTarget{}, false, err
	}
	body, target, dryRun, err := a.parseThreadMessageInput(ctx, filtered, cfg, commandName, "")
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	resolvedReplyTo, err := a.resolveMessageEventOnThread(ctx, cfg, target.ThreadID, replyTo)
	if err != nil {
		return nil, messageTarget{}, false, err
	}
	applyReplyToMessageBody(body, resolvedReplyTo)
	return body, target, dryRun, nil
}

func (a *App) runCardMessagesCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("cards messages")
	var cardIDFlag, actorIDFlag trackedString
	var maxEventsFlag trackedInt
	var mineFlag, fullIDFlag trackedBool
	var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
	fs.Var(&cardIDFlag, "card", "Card id whose messages should be listed")
	fs.Var(&cardIDFlag, "card-id", "Card id whose messages should be listed")
	fs.Var(&actorIDFlag, "actor-id", "Filter to one actor id")
	fs.Var(&mineFlag, "mine", "Filter to messages authored by active profile actor_id")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full event ids in default text output")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching messages (0 means unlimited)")
	fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
	fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
	fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
	fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
		return nil, err
	}
	positionals := fs.Args()
	cardID := firstNonEmpty(strings.TrimSpace(cardIDFlag.value), leadingCardID)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx cards messages`")
	}
	if err := validateID(cardID, "card id"); err != nil {
		return nil, err
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return nil, err
	}
	target, err := cardMessageTarget(card, cardID)
	if err != nil {
		return nil, err
	}
	eventArgs := []string{"--thread-id", target.ThreadID, "--type", "message_posted"}
	if strings.TrimSpace(actorIDFlag.value) != "" {
		eventArgs = append(eventArgs, "--actor-id", strings.TrimSpace(actorIDFlag.value))
	}
	if mineFlag.set && mineFlag.value {
		eventArgs = append(eventArgs, "--mine")
	}
	if fullIDFlag.set && fullIDFlag.value {
		eventArgs = append(eventArgs, "--full-id")
	}
	if maxEventsFlag.set {
		eventArgs = append(eventArgs, "--max-events", strconv.Itoa(maxEventsFlag.value))
	}
	if includeArchived {
		eventArgs = append(eventArgs, "--include-archived")
	}
	if archivedOnly {
		eventArgs = append(eventArgs, "--archived-only")
	}
	if includeTrashed {
		eventArgs = append(eventArgs, "--include-trashed")
	}
	if trashedOnly {
		eventArgs = append(eventArgs, "--trashed-only")
	}
	result, err := a.runEventsListCommand(ctx, eventArgs, cfg)
	if err != nil {
		return nil, err
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	body["card_id"] = target.SubjectID
	body["card_title"] = target.Title
	body["thread_id"] = target.ThreadID
	data["body"] = body
	result.Data = data
	result.Text = formatTypedCommandText("cards.messages", intValue(data["status_code"]), headerValues(data["headers"]), body, cfg.Verbose, cfg.Headers)
	return result, nil
}

func (a *App) runTopicMessagesCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingTopicID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("topics messages")
	var topicIDFlag, actorIDFlag trackedString
	var maxEventsFlag trackedInt
	var mineFlag, fullIDFlag trackedBool
	var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
	fs.Var(&topicIDFlag, "topic", "Topic id whose messages should be listed")
	fs.Var(&topicIDFlag, "topic-id", "Topic id whose messages should be listed")
	fs.Var(&actorIDFlag, "actor-id", "Filter to one actor id")
	fs.Var(&mineFlag, "mine", "Filter to messages authored by active profile actor_id")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full event ids in default text output")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching messages (0 means unlimited)")
	fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
	fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
	fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
	fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
		return nil, err
	}
	positionals := fs.Args()
	topicID := firstNonEmpty(strings.TrimSpace(topicIDFlag.value), leadingTopicID)
	if topicID == "" && len(positionals) > 0 {
		topicID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx topics messages`")
	}
	if err := validateID(topicID, "topic id"); err != nil {
		return nil, err
	}
	topic, err := a.fetchTopicBody(ctx, cfg, topicID)
	if err != nil {
		return nil, err
	}
	target, err := topicMessageTarget(topic, topicID)
	if err != nil {
		return nil, err
	}
	return a.runTargetMessagesCommand(ctx, cfg, target, "topics.messages", "Topic", actorIDFlag.value, mineFlag, fullIDFlag, maxEventsFlag, includeArchived, archivedOnly, includeTrashed, trashedOnly)
}

func (a *App) runDocMessagesCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingDocumentID, args := popLeadingPositional(args)
	fs := newSilentFlagSet("docs messages")
	var documentIDFlag, actorIDFlag trackedString
	var maxEventsFlag trackedInt
	var mineFlag, fullIDFlag trackedBool
	var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
	fs.Var(&documentIDFlag, "document", "Document id whose messages should be listed")
	fs.Var(&documentIDFlag, "document-id", "Document id whose messages should be listed")
	fs.Var(&actorIDFlag, "actor-id", "Filter to one actor id")
	fs.Var(&mineFlag, "mine", "Filter to messages authored by active profile actor_id")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full event ids in default text output")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching messages (0 means unlimited)")
	fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
	fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
	fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
	fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
		return nil, err
	}
	positionals := fs.Args()
	documentID := firstNonEmpty(strings.TrimSpace(documentIDFlag.value), leadingDocumentID)
	if documentID == "" && len(positionals) > 0 {
		documentID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs messages`")
	}
	if err := validateID(documentID, "document id"); err != nil {
		return nil, err
	}
	document, err := a.fetchDocumentBody(ctx, cfg, documentID)
	if err != nil {
		return nil, err
	}
	target, err := documentMessageTarget(document, documentID)
	if err != nil {
		return nil, err
	}
	return a.runTargetMessagesCommand(ctx, cfg, target, "docs.messages", "Document", actorIDFlag.value, mineFlag, fullIDFlag, maxEventsFlag, includeArchived, archivedOnly, includeTrashed, trashedOnly)
}

func (a *App) runTargetMessagesCommand(ctx context.Context, cfg config.Resolved, target messageTarget, commandID string, subjectLabel string, actorID string, mineFlag trackedBool, fullIDFlag trackedBool, maxEventsFlag trackedInt, includeArchived bool, archivedOnly bool, includeTrashed bool, trashedOnly bool) (*commandResult, error) {
	eventArgs := []string{"--thread-id", target.ThreadID, "--type", "message_posted"}
	if strings.TrimSpace(actorID) != "" {
		eventArgs = append(eventArgs, "--actor-id", strings.TrimSpace(actorID))
	}
	if mineFlag.set && mineFlag.value {
		eventArgs = append(eventArgs, "--mine")
	}
	if fullIDFlag.set && fullIDFlag.value {
		eventArgs = append(eventArgs, "--full-id")
	}
	if maxEventsFlag.set {
		eventArgs = append(eventArgs, "--max-events", strconv.Itoa(maxEventsFlag.value))
	}
	if includeArchived {
		eventArgs = append(eventArgs, "--include-archived")
	}
	if archivedOnly {
		eventArgs = append(eventArgs, "--archived-only")
	}
	if includeTrashed {
		eventArgs = append(eventArgs, "--include-trashed")
	}
	if trashedOnly {
		eventArgs = append(eventArgs, "--trashed-only")
	}
	result, err := a.runEventsListCommand(ctx, eventArgs, cfg)
	if err != nil {
		return nil, err
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	body["events"] = filterMessageEventsBySubjectRef(asSlice(body["events"]), target.SubjectRef)
	body["returned_events"] = len(asSlice(body["events"]))
	body["subject_kind"] = target.SubjectKind
	body["subject_id"] = target.SubjectID
	body["subject_title"] = target.Title
	body["thread_id"] = target.ThreadID
	data["body"] = body
	result.Data = data
	result.Text = formatTypedCommandText(commandID, intValue(data["status_code"]), headerValues(data["headers"]), body, cfg.Verbose, cfg.Headers)
	return result, nil
}

func filterMessageEventsBySubjectRef(events []any, subjectRef string) []any {
	subjectRef = strings.TrimSpace(subjectRef)
	if subjectRef == "" {
		return events
	}
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		refs := stringList(event["refs"])
		if stringSliceContains(refs, subjectRef) {
			out = append(out, raw)
			continue
		}
		payload := asMap(event["payload"])
		if strings.TrimSpace(anyString(payload["subject_ref"])) == subjectRef {
			out = append(out, raw)
		}
	}
	return out
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func popLeadingPositional(args []string) (string, []string) {
	if len(args) == 0 {
		return "", args
	}
	first := strings.TrimSpace(args[0])
	if first == "" || strings.HasPrefix(first, "-") {
		return "", args
	}
	out := make([]string, 0, len(args)-1)
	out = append(out, args[1:]...)
	return first, out
}

func extractReplyTarget(args []string, commandName string) (string, []string, error) {
	replyTo := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		token := strings.TrimSpace(args[i])
		if token == "--to" {
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" || strings.HasPrefix(strings.TrimSpace(args[i+1]), "-") {
				return "", nil, errnorm.Usage("invalid_flags", fmt.Sprintf("--to requires a value for `anx %s`", commandName))
			}
			if replyTo != "" {
				return "", nil, errnorm.Usage("invalid_flags", fmt.Sprintf("--to can only be supplied once for `anx %s`", commandName))
			}
			replyTo = strings.TrimSpace(args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(token, "--to=") {
			if replyTo != "" {
				return "", nil, errnorm.Usage("invalid_flags", fmt.Sprintf("--to can only be supplied once for `anx %s`", commandName))
			}
			replyTo = strings.TrimSpace(strings.TrimPrefix(token, "--to="))
			continue
		}
		filtered = append(filtered, args[i])
	}
	return replyTo, filtered, nil
}

func buildMessagePostedBody(cfg config.Resolved, actorFlag string, target messageTarget, message string, summary string, extraRefs []string, replyToEventID string) (map[string]any, error) {
	actorID, err := resolveActorIDAlias(actorFlag, cfg)
	if err != nil {
		return nil, err
	}
	if actorID == "" {
		actorID = strings.TrimSpace(cfg.ActorID)
	}
	if actorID == "" {
		return nil, errnorm.Usage("invalid_request", "message commands require an actor_id; pass --actor-id or use a profile with actor_id")
	}
	refs := []string{target.SubjectRef, "thread:" + target.ThreadID}
	if target.BoardRef != "" {
		refs = append(refs, target.BoardRef)
	}
	refs = append(refs, normalizedStringsOrEmpty(extraRefs)...)
	payload := map[string]any{
		"kind":         firstNonEmpty(target.MessageKind, target.SubjectKind+"_message"),
		"text":         message,
		"subject_ref":  target.SubjectRef,
		"subject_kind": target.SubjectKind,
		"subject_id":   target.SubjectID,
	}
	if replyToEventID != "" {
		payload["reply_to_event_id"] = replyToEventID
		refs = append(refs, "event:"+replyToEventID)
	}
	event := map[string]any{
		"type":       "message_posted",
		"actor_id":   actorID,
		"thread_id":  target.ThreadID,
		"thread_ref": "thread:" + target.ThreadID,
		"refs":       uniqueStrings(refs),
		"summary":    firstNonEmpty(strings.TrimSpace(summary), truncatePreview(message)),
		"payload":    payload,
		"provenance": map[string]any{"sources": []any{"event:anx-cli"}},
	}
	return map[string]any{"actor_id": actorID, "event": event}, nil
}

func topicMessageTarget(topic map[string]any, fallbackTopicID string) (messageTarget, error) {
	topicID := firstNonEmpty(strings.TrimSpace(anyString(topic["id"])), strings.TrimSpace(fallbackTopicID))
	if topicID == "" {
		return messageTarget{}, errnorm.Usage("invalid_request", "topic response did not include topic id")
	}
	threadID := strings.TrimSpace(anyString(topic["thread_id"]))
	if threadID == "" {
		return messageTarget{}, errnorm.Usage("invalid_request", "topic response did not include thread_id for messages")
	}
	return messageTarget{
		SubjectKind: "topic",
		SubjectID:   topicID,
		SubjectRef:  "topic:" + topicID,
		Title:       strings.TrimSpace(anyString(topic["title"])),
		ThreadID:    threadID,
		MessageKind: "topic_message",
	}, nil
}

func documentMessageTarget(document map[string]any, fallbackDocumentID string) (messageTarget, error) {
	documentID := firstNonEmpty(strings.TrimSpace(anyString(document["id"])), strings.TrimSpace(fallbackDocumentID))
	if documentID == "" {
		return messageTarget{}, errnorm.Usage("invalid_request", "document response did not include document id")
	}
	threadID := strings.TrimSpace(anyString(document["thread_id"]))
	if threadID == "" {
		return messageTarget{}, errnorm.Usage("invalid_request", "document response did not include thread_id for messages")
	}
	return messageTarget{
		SubjectKind: "document",
		SubjectID:   documentID,
		SubjectRef:  "document:" + documentID,
		Title:       strings.TrimSpace(anyString(document["title"])),
		ThreadID:    threadID,
		MessageKind: "document_message",
	}, nil
}

func cardMessageTarget(card map[string]any, fallbackCardID string) (messageTarget, error) {
	cardID := firstNonEmpty(strings.TrimSpace(anyString(card["id"])), strings.TrimSpace(fallbackCardID))
	if cardID == "" {
		return messageTarget{}, errnorm.Usage("invalid_request", "card response did not include card id")
	}
	threadID := strings.TrimSpace(anyString(card["thread_id"]))
	if threadID == "" {
		return messageTarget{}, errnorm.Usage("invalid_request", "card response did not include thread_id for messages")
	}
	boardRef := strings.TrimSpace(anyString(card["board_ref"]))
	return messageTarget{
		SubjectKind: "card",
		SubjectID:   cardID,
		SubjectRef:  "card:" + cardID,
		Title:       strings.TrimSpace(anyString(card["title"])),
		ThreadID:    threadID,
		BoardRef:    boardRef,
	}, nil
}

func threadMessageTarget(threadID string) messageTarget {
	threadID = strings.TrimSpace(threadID)
	return messageTarget{
		SubjectKind: "thread",
		SubjectID:   threadID,
		SubjectRef:  "thread:" + threadID,
		ThreadID:    threadID,
		MessageKind: "thread_message",
	}
}

func applyReplyToMessageBody(body map[string]any, replyToEventID string) {
	event := asMap(body["event"])
	if event == nil {
		return
	}
	payload := asMap(event["payload"])
	if payload == nil {
		payload = map[string]any{}
		event["payload"] = payload
	}
	payload["reply_to_event_id"] = replyToEventID
	refs := stringList(event["refs"])
	refs = append(refs, "event:"+replyToEventID)
	event["refs"] = uniqueStrings(refs)
}

func (a *App) resolveMessageEventOnThread(ctx context.Context, cfg config.Resolved, threadID string, rawEventID string) (string, error) {
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "threads timeline", "threads.timeline", "thread_id", threadID, threadIDLookupSpec, nil, nil)
	if err != nil {
		return "", err
	}
	body := extractNestedMap(asMap(result.Data), "body")
	events := asSlice(body["events"])
	matches := make([]string, 0, 2)
	for _, raw := range events {
		event := asMap(raw)
		if event == nil || strings.TrimSpace(anyString(event["type"])) != "message_posted" {
			continue
		}
		id := strings.TrimSpace(anyString(event["id"]))
		if id == "" {
			continue
		}
		if id == rawEventID {
			return id, nil
		}
		if strings.HasPrefix(id, rawEventID) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sortStrings(matches)
		return "", ambiguousResourceIDError(rawEventID, eventIDLookupSpec, matches)
	}
	return "", errnorm.Usage("not_found", fmt.Sprintf("message %q was not found on thread %s", rawEventID, threadID))
}

func (a *App) readMessageText(body string, bodyFile string, commandName string) (string, error) {
	body = strings.TrimSpace(body)
	bodyFile = strings.TrimSpace(bodyFile)
	sources := 0
	if body != "" {
		sources++
	}
	if bodyFile != "" {
		sources++
	}
	if sources > 1 {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("provide only one of --body or --body-file for `anx %s`", commandName))
	}
	if body != "" {
		return body, nil
	}
	if bodyFile != "" {
		content, err := a.readRawFile(bodyFile)
		if err != nil {
			return "", err
		}
		text := string(content)
		if strings.TrimSpace(text) == "" {
			return "", errnorm.Usage("invalid_request", fmt.Sprintf("message body file is empty for `anx %s`", commandName))
		}
		return text, nil
	}
	stdin, err := a.readStdinBody()
	if err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read stdin", err)
	}
	if len(stdin) > 0 {
		return string(stdin), nil
	}
	return "", errnorm.Usage("invalid_request", fmt.Sprintf("message body is required for `anx %s`; use --body, --body-file, or pipe text on stdin", commandName))
}

func (a *App) readExplicitMessageText(body string, bodyFile string, commandName string) (string, error) {
	body = strings.TrimSpace(body)
	bodyFile = strings.TrimSpace(bodyFile)
	if body != "" && bodyFile != "" {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("provide only one of --body or --body-file for `anx %s`", commandName))
	}
	if body != "" {
		return body, nil
	}
	if bodyFile != "" {
		content, err := a.readRawFile(bodyFile)
		if err != nil {
			return "", err
		}
		text := string(content)
		if strings.TrimSpace(text) == "" {
			return "", errnorm.Usage("invalid_request", fmt.Sprintf("message body file is empty for `anx %s`", commandName))
		}
		return text, nil
	}
	return "", errnorm.Usage("invalid_request", fmt.Sprintf("message body is required for `anx %s`; use --body or --body-file", commandName))
}

func (a *App) decorateMessageWriteResult(result *commandResult, callErr error, cfg config.Resolved, commandID string, card map[string]any) *commandResult {
	if result == nil || callErr != nil {
		return result
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	event := extractNestedMap(body, "event")
	target, targetErr := cardMessageTarget(card, strings.TrimSpace(anyString(card["id"])))
	if targetErr == nil {
		body["card_id"] = target.SubjectID
		body["card_title"] = target.Title
		body["thread_id"] = target.ThreadID
		body["refs"] = stringList(event["refs"])
	}
	data["body"] = body
	result.Data = data
	result.Text = formatTypedCommandText(commandID, intValue(data["status_code"]), headerValues(data["headers"]), body, cfg.Verbose, cfg.Headers)
	return result
}

func decorateThreadMessageWriteResult(result *commandResult, callErr error, cfg config.Resolved, commandID string, target messageTarget) *commandResult {
	if result == nil || callErr != nil {
		return result
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	event := extractNestedMap(body, "event")
	if target.SubjectKind != "" && target.SubjectKind != "thread" {
		body["subject_kind"] = target.SubjectKind
		body["subject_id"] = target.SubjectID
		body["subject_title"] = target.Title
	}
	body["thread_id"] = target.ThreadID
	body["refs"] = stringList(event["refs"])
	data["body"] = body
	result.Data = data
	result.Text = formatTypedCommandText(commandID, intValue(data["status_code"]), headerValues(data["headers"]), body, cfg.Verbose, cfg.Headers)
	return result
}

func messageWriteText(body any, subjectLabel string) string {
	root := asMap(body)
	event := extractNestedMap(root, "event")
	lines := []string{"Message posted."}
	switch subjectLabel {
	case "Card":
		title := strings.TrimSpace(anyString(root["card_title"]))
		cardID := strings.TrimSpace(anyString(root["card_id"]))
		if title != "" && cardID != "" {
			lines = append(lines, "Card: "+title+" ("+cardID+")")
		} else if cardID != "" {
			lines = append(lines, "Card: "+cardID)
		}
	case "Topic", "Document":
		title := strings.TrimSpace(anyString(root["subject_title"]))
		id := strings.TrimSpace(anyString(root["subject_id"]))
		if title != "" && id != "" {
			lines = append(lines, subjectLabel+": "+title+" ("+id+")")
		} else if id != "" {
			lines = append(lines, subjectLabel+": "+id)
		}
	case "Thread":
		if threadID := strings.TrimSpace(anyString(root["thread_id"])); threadID != "" {
			lines = append(lines, "Thread: "+threadID)
		}
	}
	if subjectLabel == "Card" || subjectLabel == "Topic" || subjectLabel == "Document" {
		if threadID := strings.TrimSpace(anyString(root["thread_id"])); threadID != "" {
			lines = append(lines, "Thread: "+threadID)
		}
	}
	if eventID := strings.TrimSpace(anyString(event["id"])); eventID != "" {
		lines = append(lines, "Event: "+eventID)
	}
	return strings.Join(lines, "\n")
}

func formatTopicMessages(body any) string {
	return formatSubjectMessages(body, "Topic")
}

func formatDocMessages(body any) string {
	return formatSubjectMessages(body, "Document")
}

func formatSubjectMessages(body any, label string) string {
	root := asMap(body)
	lines := []string{label + " messages"}
	title := strings.TrimSpace(anyString(root["subject_title"]))
	id := strings.TrimSpace(anyString(root["subject_id"]))
	if title != "" && id != "" {
		lines = append(lines, label+": "+title+" ("+id+")")
	} else if id != "" {
		lines = append(lines, label+": "+id)
	}
	lines = appendScalar(lines, "thread_id", root, "thread_id")
	lines = appendScalar(lines, "total_events", root, "total_events")
	lines = appendScalar(lines, "returned_events", root, "returned_events")
	lines = appendEventListSection(lines, "messages", asSlice(root["events"]), asBool(root["full_id"]))
	return strings.Join(lines, "\n")
}

func formatCardsMessages(body any) string {
	root := asMap(body)
	lines := []string{"Card messages"}
	title := strings.TrimSpace(anyString(root["card_title"]))
	cardID := strings.TrimSpace(anyString(root["card_id"]))
	if title != "" && cardID != "" {
		lines = append(lines, "Card: "+title+" ("+cardID+")")
	} else if cardID != "" {
		lines = append(lines, "Card: "+cardID)
	}
	lines = appendScalar(lines, "thread_id", root, "thread_id")
	lines = appendScalar(lines, "total_events", root, "total_events")
	lines = appendScalar(lines, "returned_events", root, "returned_events")
	lines = appendEventListSection(lines, "messages", asSlice(root["events"]), asBool(root["full_id"]))
	return strings.Join(lines, "\n")
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
