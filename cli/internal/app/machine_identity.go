package app

import "strings"

type machineCommandIdentity struct {
	Command   string
	CommandID string
}

var machineCommandIdentityByPath = map[string]machineCommandIdentity{
	"events list":     {Command: "events list", CommandID: "threads.timeline"},
	"events get":      {Command: "events get", CommandID: "events.get"},
	"events stream":   {Command: "events stream", CommandID: "events.stream"},
	"events tail":     {Command: "events stream", CommandID: "events.stream"},
	"inbox stream":    {Command: "inbox stream", CommandID: "inbox.stream"},
	"inbox tail":      {Command: "inbox stream", CommandID: "inbox.stream"},
	"threads context": {Command: "threads context", CommandID: "threads.context"},
	"threads inspect": {Command: "threads inspect", CommandID: "threads.inspect"},
	"threads workspace": {
		Command:   "threads workspace",
		CommandID: "threads.workspace",
	},
	"threads patch": {
		Command:   "threads patch",
		CommandID: "threads.patch.propose",
	},
	"threads apply": {
		Command:   "threads apply",
		CommandID: "threads.patch.apply",
	},
	"threads recommendations": {
		Command:   "threads recommendations",
		CommandID: "threads.recommendations",
	},
	"commitments update": {
		Command:   "commitments update",
		CommandID: "commitments.patch.propose",
	},
	"commitments apply": {
		Command:   "commitments apply",
		CommandID: "commitments.patch.apply",
	},
	"docs update": {
		Command:   "docs update",
		CommandID: "docs.update.propose",
	},
	"docs apply": {
		Command:   "docs apply",
		CommandID: "docs.update.apply",
	},
}

func resolveMachineCommandIdentity(command string) machineCommandIdentity {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
	if normalized == "" {
		return machineCommandIdentity{Command: "root"}
	}
	if identity, ok := machineCommandIdentityByPath[normalized]; ok {
		return identity
	}
	commandID := strings.ReplaceAll(normalized, " ", ".")
	return machineCommandIdentity{Command: normalized, CommandID: commandID}
}
