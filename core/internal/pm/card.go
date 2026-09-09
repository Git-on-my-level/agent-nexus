package pm

import (
	"fmt"
	"strconv"
	"strings"
)

const bindFirstText = "This channel identity is not bound to a workspace principal. An operator must run `anx pm bindings create` before the PM will accept messages."

func decisionCardText(d Decision) string {
	return fmt.Sprintf("Decision %s\nRevision %d\n%s\nWork: %s", d.ID, d.Revision, d.Instruction, d.WorkRef)
}

func telegramDecisionMarkup(d Decision) map[string]any {
	return map[string]any{
		"inline_keyboard": []any{
			[]any{
				map[string]any{"text": "Approve", "callback_data": fmt.Sprintf("a:%s:%d", d.ID, d.Revision)},
				map[string]any{"text": "Reject", "callback_data": fmt.Sprintf("r:%s:%d", d.ID, d.Revision)},
			},
		},
	}
}

func discordDecisionComponents(d Decision) []any {
	return []any{
		map[string]any{
			"type": 1,
			"components": []any{
				map[string]any{"type": 2, "style": 3, "label": "Approve", "custom_id": fmt.Sprintf("pm:a:%s:%d", d.ID, d.Revision)},
				map[string]any{"type": 2, "style": 4, "label": "Reject", "custom_id": fmt.Sprintf("pm:r:%s:%d", d.ID, d.Revision)},
			},
		},
	}
}

func decisionReplyMarkup(transport string, d Decision) map[string]any {
	switch transport {
	case "telegram":
		return telegramDecisionMarkup(d)
	case "discord":
		return map[string]any{"components": discordDecisionComponents(d)}
	default:
		return nil
	}
}

func parseDecisionCallback(data string) (approve bool, id string, revision int, ok bool) {
	data = strings.TrimSpace(data)
	data = strings.TrimPrefix(data, "pm:")
	parts := strings.Split(data, ":")
	if len(parts) < 3 {
		return false, "", 0, false
	}
	switch parts[0] {
	case "a", "approve":
		approve = true
	case "r", "reject":
		approve = false
	default:
		return false, "", 0, false
	}
	rev, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return false, "", 0, false
	}
	id = strings.Join(parts[1:len(parts)-1], ":")
	if id == "" {
		return false, "", 0, false
	}
	return approve, id, rev, true
}
