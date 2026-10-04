package app

import "strings"

// commandHelpTopic distinguishes help options from string flag values using
// the same flag kinds as usage preflight. Unknown flags fail closed: they
// retain normal validation and workspace resolution rather than gaining a
// help exemption. The caller must render help locally instead of dispatching
// the original argv to a command that could perform network I/O.
func commandHelpTopic(args []string) (string, bool) {
	if len(args) == 0 || args[0] == "help" {
		return "", false
	}
	prefix := ""
	commandArgs := args
	if args[0] == "debug" {
		prefix = "debug "
		commandArgs = args[1:]
	}
	if len(commandArgs) == 0 {
		return "", false
	}
	if rewritten, ok := applyCommandShapeCompatibilityAlias(commandArgs); ok {
		commandArgs = rewritten
	}
	path, flags, known := matchPreflightFlagSpec(commandArgs)
	if !known {
		// A plain command path followed by help has no flag values to confuse.
		if isTrailingHelpOnlyInvocation(commandArgs) {
			end := len(commandArgs)
			for end > 0 && isHelpToken(commandArgs[end-1]) {
				end--
			}
			topic := prefix + strings.Join(commandArgs[:end], " ")
			return topic, true
		}
		return "", false
	}
	specs := preflightFlagSpecs()[path]
	for i := 0; i < len(flags); i++ {
		token := flags[i]
		if token == "--" {
			break
		}
		if token == "--help" || token == "-h" {
			return prefix + path, true
		}
		name, _, inline, option := parseLongOptionToken(token)
		if !option {
			continue
		}
		spec, ok := specs[name]
		if !ok {
			return "", false
		}
		if spec.kind == preflightFlagBool && inline {
			_, value, _, _ := parseLongOptionToken(token)
			if _, err := strconvParseBool(value); err != nil {
				return "", false
			}
		}
		if spec.kind == preflightFlagString && !inline {
			// Go flag parsers consume even a --help/-h token as a string value.
			if i+1 >= len(flags) {
				return "", false
			}
			i++
		}
	}
	// Preserve the existing bare `help` command-path spelling, but never treat
	// arbitrary positional refs or values as options.
	if isTrailingHelpOnlyInvocation(commandArgs) {
		end := len(commandArgs)
		for end > 0 && isHelpToken(commandArgs[end-1]) {
			end--
		}
		topic := prefix + strings.Join(commandArgs[:end], " ")
		return topic, true
	}
	return "", false
}
