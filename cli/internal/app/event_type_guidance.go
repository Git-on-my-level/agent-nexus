// Embedded event-type guidance table used to explain and validate event type
// filters and events create input.

package app

import (
	"strings"
)

type eventTypeGuidance struct {
	Type             string
	Group            string
	Summary          string
	Constraints      []string
	PreferredCommand string
}

var eventTypeGroupOrder = []string{
	"Communication",
	"Topics And Documents",
	"Boards And Cards",
	"Exceptions",
	"Packet Lifecycle",
	"Inbox Lifecycle",
}

var eventTypeGroupDescriptions = map[string]string{
	"Communication":        "Direct communication or important non-structured information.",
	"Topics And Documents": "Durable work-subject and document lifecycle signals.",
	"Boards And Cards":     "Board and card workflow signals.",
	"Exceptions":           "Surface problems, risks, or escalations.",
	"Inbox Lifecycle":      "Inbox lifecycle facts, usually emitted by higher-level commands.",
}

var knownEventTypeGuidance = []eventTypeGuidance{
	{
		Type:             "message_posted",
		Group:            "Communication",
		PreferredCommand: "anx cards|topics|docs message|reply, or anx threads message|reply",
		Summary:          "Use for low-level communication records that belong on a backing thread; prefer topic/document/card message commands for ordinary discussion.",
		Constraints: []string{
			"thread_id is required when posting directly to a backing thread timeline.",
			"Use domain commands such as `anx cards message card:<handle>`, `anx topics message topic:<handle>`, or `anx docs message doc:<handle>` for ordinary discussion; they fill actor_id, thread_id, and refs.",
			"Use this type for messages, replies, or important non-structured information that should read like direct communication on a backing thread.",
			`event.refs may include "event:<parent-event-handle>" for replies and "artifact:<artifact-handle>" mentions.`,
		},
	},
	{
		Type:             "human_attention_requested",
		Group:            "Inbox Lifecycle",
		PreferredCommand: "anx ask|review|escalate",
		Summary:          "Use the human command group to ask for operator attention, review, or escalation.",
		Constraints: []string{
			`event.refs must include "thread:<thread-handle>" and a typed subject ref such as "topic:<topic-handle>", "card:<card-handle>", or "document:<document-handle>".`,
			`event.payload must include "kind", "title", "body", "subject_ref", and ordered "response_proposals".`,
		},
	},
	{
		Type:             "human_attention_responded",
		Group:            "Inbox Lifecycle",
		PreferredCommand: "anx inbox respond",
		Summary:          "Records a human response and is the only source for Completed Inbox history.",
		Constraints: []string{
			`event.refs must include "inbox:<inbox-item-handle>".`,
			`event.payload must include "inbox_item_id", "kind", "request_event_ref", and "response_text".`,
		},
	},
	{
		Type:             "human_attention_withdrawn",
		Group:            "Inbox Lifecycle",
		PreferredCommand: "anx ask withdraw",
		Summary:          "Records an ask withdrawal by its requesting agent; it is not a human response or completed answer.",
		Constraints: []string{
			`event.refs must include "event:<request-event-id>".`,
			`event.payload must include "request_event_id", "request_event_ref", "withdrawn_by_actor_id", and "reason".`,
		},
	},
	{
		Type:  "topic_created",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "topic:<topic-handle>".`,
		},
	},
	{
		Type:  "topic_updated",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "topic:<topic-handle>".`,
		},
	},
	{
		Type:  "document_created",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "document:<document-handle>", "document_revision:<revision-handle>", and "artifact:<artifact-handle>".`,
		},
	},
	{
		Type:  "document_revised",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "document:<document-handle>", "document_revision:<revision-handle>", and "artifact:<artifact-handle>".`,
		},
	},
	{
		Type:  "document_trashed",
		Group: "Topics And Documents",
		Constraints: []string{
			`event.refs must include "document:<document-handle>".`,
		},
	},
	{
		Type:  "board_created",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "board:<board-handle>".`,
		},
	},
	{
		Type:  "board_updated",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "board:<board-handle>".`,
		},
	},
	{
		Type:  "card_created",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card-handle>" and "board:<board-handle>".`,
		},
	},
	{
		Type:  "card_updated",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card-handle>" and "board:<board-handle>".`,
		},
	},
	{
		Type:  "card_moved",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card-handle>" and "board:<board-handle>".`,
			`event.payload must include "column_key".`,
		},
	},
	{
		Type:  "card_resolved",
		Group: "Boards And Cards",
		Constraints: []string{
			`event.refs must include "card:<card-handle>" and "board:<board-handle>".`,
			`event.payload must include "resolution".`,
		},
	},
	{
		Type:  "exception_raised",
		Group: "Exceptions",
		Constraints: []string{
			`event.payload must include "subtype".`,
			"Attach topic/card/document refs when possible so the exception stays connected to the primary resource surface.",
		},
	},
	{
		Type:             "human_attention_requested",
		Group:            "Human Coordination",
		PreferredCommand: "anx ask|review|escalate",
		Constraints: []string{
			`event.refs must include "thread:<thread-handle>".`,
			`event.payload must include "kind", "title", "subject_ref", "requester_actor_id", and ordered "response_proposals" (1–6 strings, first is recommended).`,
		},
	},
	{
		Type:             "human_attention_responded",
		Group:            "Human Coordination",
		PreferredCommand: "anx inbox respond",
		Constraints: []string{
			`event.refs must include "inbox:<inbox-item-handle>".`,
			`event.payload must include "inbox_item_id", "kind", "response_text", and "responding_actor_id".`,
		},
	},
	{
		Type:             "human_attention_withdrawn",
		Group:            "Human Coordination",
		PreferredCommand: "anx ask withdraw",
		Constraints: []string{
			`event.refs must include "event:<request-event-id>".`,
			`event.payload must include "request_event_id", "request_event_ref", "withdrawn_by_actor_id", and "reason".`,
		},
	},
}

func eventTypeGuidanceFor(eventType string) (eventTypeGuidance, bool) {
	eventType = strings.TrimSpace(eventType)
	for _, guidance := range knownEventTypeGuidance {
		if guidance.Type == eventType {
			return guidance, true
		}
	}
	return eventTypeGuidance{}, false
}

func knownEventTypeNames() []string {
	names := make([]string, 0, len(knownEventTypeGuidance))
	for _, guidance := range knownEventTypeGuidance {
		names = append(names, guidance.Type)
	}
	return names
}
