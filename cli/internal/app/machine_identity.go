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
}

func resolveMachineCommandIdentity(command string) machineCommandIdentity {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
	if normalized == "" {
		return machineCommandIdentity{Command: "root"}
	}
	if identity, ok := machineCommandIdentityByPath[normalized]; ok {
		return identity
	}
	return machineCommandIdentity{Command: normalized}
}
