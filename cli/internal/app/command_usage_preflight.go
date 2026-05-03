package app

import (
	"flag"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/errnorm"
)

func preflightConfigIndependentUsage(args []string) (string, error) {
	if len(args) < 2 || hasHelpToken(args) {
		return "", nil
	}
	resource := strings.TrimSpace(args[0])
	sub := strings.TrimSpace(args[1])
	commandName := resource + " " + sub
	commandArgs := args[2:]

	switch resource {
	case "topics":
		switch sub {
		case "message":
			return commandName, preflightMessageWriteFlags(commandName, commandArgs, "topic")
		case "reply":
			return commandName, preflightMessageReplyFlags(commandName, commandArgs, "topic")
		case "messages":
			return commandName, preflightMessagesListFlags(commandName, commandArgs, "topic")
		}
	case "docs":
		switch sub {
		case "message":
			return commandName, preflightMessageWriteFlags(commandName, commandArgs, "document")
		case "reply":
			return commandName, preflightMessageReplyFlags(commandName, commandArgs, "document")
		case "messages":
			return commandName, preflightMessagesListFlags(commandName, commandArgs, "document")
		}
	case "cards":
		switch sub {
		case "message":
			return commandName, preflightMessageWriteFlags(commandName, commandArgs, "card")
		case "reply":
			return commandName, preflightMessageReplyFlags(commandName, commandArgs, "card")
		case "messages":
			return commandName, preflightMessagesListFlags(commandName, commandArgs, "card")
		}
	case "threads":
		switch sub {
		case "message":
			return commandName, preflightMessageWriteFlags(commandName, commandArgs, "thread")
		case "reply":
			return commandName, preflightMessageReplyFlags(commandName, commandArgs, "thread")
		}
	}
	return "", nil
}

func preflightMessageWriteFlags(commandName string, args []string, idFlagName string) error {
	_, args = popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var idFlag, bodyFlag, bodyFileFlag, summaryFlag, actorIDFlag trackedString
	var refFlags trackedStrings
	var dryRunFlag trackedBool
	registerMessageTargetFlags(fs, idFlagName, &idFlag)
	fs.Var(&bodyFlag, "body", "Message body text")
	fs.Var(&bodyFileFlag, "body-file", "Load message body text from a local file")
	fs.Var(&summaryFlag, "summary", "Optional short event summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id; defaults from the active profile")
	fs.Var(&refFlags, "ref", "Additional typed ref to attach to the message, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return errnorm.Usage("invalid_flags", err.Error())
	}
	return nil
}

func preflightMessageReplyFlags(commandName string, args []string, idFlagName string) error {
	_, filtered, err := extractReplyTarget(args, commandName)
	if err != nil {
		return err
	}
	return preflightMessageWriteFlags(commandName, filtered, idFlagName)
}

func preflightMessagesListFlags(commandName string, args []string, idFlagName string) error {
	_, args = popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var idFlag, actorIDFlag trackedString
	var maxEventsFlag trackedInt
	var mineFlag, fullIDFlag trackedBool
	var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
	registerMessageTargetFlags(fs, idFlagName, &idFlag)
	fs.Var(&actorIDFlag, "actor-id", "Filter to one actor id")
	fs.Var(&mineFlag, "mine", "Filter to messages authored by active profile actor_id")
	fs.Var(&fullIDFlag, "full-id", "Render full event ids in default text output")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching messages")
	fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
	fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
	fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
	fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
	if err := fs.Parse(args); err != nil {
		return errnorm.Usage("invalid_flags", err.Error())
	}
	if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
		return err
	}
	return nil
}

func registerMessageTargetFlags(fs interface {
	Var(value flag.Value, name string, usage string)
}, idFlagName string, target *trackedString) {
	switch idFlagName {
	case "topic":
		fs.Var(target, "topic", "Topic id")
		fs.Var(target, "topic-id", "Topic id")
	case "document":
		fs.Var(target, "document", "Document id")
		fs.Var(target, "document-id", "Document id")
	case "card":
		fs.Var(target, "card", "Card id")
		fs.Var(target, "card-id", "Card id")
	case "thread":
		fs.Var(target, "thread", "Thread id")
		fs.Var(target, "thread-id", "Thread id")
	default:
		panic(fmt.Sprintf("unknown message target flag %q", idFlagName))
	}
}

func hasHelpToken(args []string) bool {
	for _, arg := range args {
		if isHelpToken(arg) {
			return true
		}
	}
	return false
}
