package app

import "strings"

type commandShapeCompatibilityAlias struct {
	from                []string
	to                  []string
	requireTrailingArgs bool
}

var commandShapeCompatibilityAliases = []commandShapeCompatibilityAlias{
	{
		from: []string{"packets", "receipts", "create"},
		to:   []string{"receipts", "create"},
	},
	{
		from: []string{"packets", "reviews", "create"},
		to:   []string{"reviews", "create"},
	},
	{
		from:                []string{"artifacts", "content", "get"},
		to:                  []string{"artifacts", "content"},
		requireTrailingArgs: true,
	},
	{
		from: []string{"topics", "update"},
		to:   []string{"topics", "patch"},
	},
}

func applyCommandShapeCompatibilityAlias(args []string) ([]string, bool) {
	for _, alias := range commandShapeCompatibilityAliases {
		if !hasExactTokenPrefix(args, alias.from) {
			continue
		}
		if alias.requireTrailingArgs && len(args) == len(alias.from) {
			continue
		}
		rewritten := make([]string, 0, len(alias.to)+len(args)-len(alias.from))
		rewritten = append(rewritten, alias.to...)
		rewritten = append(rewritten, args[len(alias.from):]...)
		return rewritten, true
	}
	return args, false
}

func hasExactTokenPrefix(args []string, prefix []string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if strings.TrimSpace(args[i]) != strings.TrimSpace(prefix[i]) {
			return false
		}
	}
	return true
}
