package app

import "strings"

type machineCommandIdentity struct {
	Command   string
	CommandID string
}

var machineCommandIdentityByPath = map[string]machineCommandIdentity{
	"human ask":       {Command: "human ask", CommandID: "events.create"},
	"human review":    {Command: "human review", CommandID: "events.create"},
	"human escalate":  {Command: "human escalate", CommandID: "events.create"},
	"secret list":     {Command: "secret list", CommandID: "secrets.list"},
	"secret create":   {Command: "secret create", CommandID: "secrets.create"},
	"secret get":      {Command: "secret get", CommandID: "secrets.get"},
	"secret delete":   {Command: "secret delete", CommandID: "secrets.delete"},
	"secret exec":          {Command: "secret exec", CommandID: "secrets.reveal-batch"},
	"secret get --reveal":  {Command: "secret get --reveal", CommandID: "secrets.reveal"},
	"secret update":        {Command: "secret update", CommandID: "secrets.update"},
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
	"docs revise": {
		Command:   "docs revise",
		CommandID: "docs.revisions.create.propose",
	},
	"docs revise apply": {
		Command:   "docs revise",
		CommandID: "docs.revisions.create.apply",
	},
	"docs history": {
		Command:   "docs history",
		CommandID: "docs.revisions.list",
	},
	"cards history": {
		Command:   "cards history",
		CommandID: "cards.revisions.list",
	},
	"cards revision get": {
		Command:   "cards revision get",
		CommandID: "cards.revisions.get",
	},
	"docs revision get": {
		Command:   "docs revision get",
		CommandID: "docs.revisions.get",
	},
	"topics message":  {Command: "topics message", CommandID: "events.create"},
	"topics reply":    {Command: "topics reply", CommandID: "events.create"},
	"topics messages": {Command: "topics messages", CommandID: "events.list"},
	"cards message":   {Command: "cards message", CommandID: "events.create"},
	"cards reply":     {Command: "cards reply", CommandID: "events.create"},
	"cards messages":  {Command: "cards messages", CommandID: "events.list"},
	"docs message":    {Command: "docs message", CommandID: "events.create"},
	"docs reply":      {Command: "docs reply", CommandID: "events.create"},
	"docs messages":   {Command: "docs messages", CommandID: "events.list"},
	"cards revise":    {Command: "cards revise", CommandID: "cards.revisions.create"},
	"cards assign":    {Command: "cards assign", CommandID: "cards.patch"},
	"cards resolve":   {Command: "cards resolve", CommandID: "cards.move"},
	"cards reopen":    {Command: "cards reopen", CommandID: "cards.move"},
	"threads message": {Command: "threads message", CommandID: "events.create"},
	"threads reply":   {Command: "threads reply", CommandID: "events.create"},
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
