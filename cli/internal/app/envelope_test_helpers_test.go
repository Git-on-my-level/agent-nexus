package app

import "strings"

func machineEnvelopeCommandID(envelope map[string]any) string {
	command := anyStringValue(envelope["command"])
	command = strings.TrimPrefix(command, "debug ")
	return resolveMachineCommandIdentity(command).CommandID
}
