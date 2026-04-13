package app

import "strings"

type machineCommandIdentity struct {
	Command   string
	CommandID string
}

var machineCommandIdentityByPath = map[string]machineCommandIdentity{
	"secret list":   {Command: "secret list", CommandID: "secrets.list"},
	"secret create": {Command: "secret create", CommandID: "secrets.create"},
	"secret get":    {Command: "secret get", CommandID: "secrets.get"},
	"secret delete": {Command: "secret delete", CommandID: "secrets.delete"},
	"secret exec":   {Command: "secret exec", CommandID: "secrets.exec"},
	"events list":     {Command: "events list", CommandID: "events.list"},
	"events get":      {Command: "events get", CommandID: "events.get"},
	"events stream":   {Command: "events stream", CommandID: "events.stream"},
	"events tail":     {Command: "events stream", CommandID: "events.stream"},
	"inbox stream":    {Command: "inbox stream", CommandID: "inbox.stream"},
	"inbox tail":      {Command: "inbox stream", CommandID: "inbox.stream"},
	"threads context": {Command: "threads context", CommandID: "threads.context"},
	"threads get":     {Command: "threads get", CommandID: "threads.inspect"},
	"threads inspect": {Command: "threads inspect", CommandID: "threads.inspect"},
	"threads workspace": {
		Command:   "threads workspace",
		CommandID: "threads.workspace",
	},
	"threads review": {
		Command:   "threads review",
		CommandID: "threads.review",
	},
	"threads recommendations": {
		Command:   "threads recommendations",
		CommandID: "threads.recommendations",
	},
	"docs propose-update": {
		Command:   "docs propose-update",
		CommandID: "docs.revisions.create.propose",
	},
	"docs apply": {
		Command:   "docs apply",
		CommandID: "docs.revisions.create.apply",
	},
	"docs history": {
		Command:   "docs history",
		CommandID: "docs.revisions.list",
	},
	"docs revision get": {
		Command:   "docs revision get",
		CommandID: "docs.revisions.get",
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
