package server

const actorStatementEventIDPlaceholder = "<event_id>"

func actorStatementProvenance() map[string]any {
	return map[string]any{
		"sources": []string{"actor_statement:" + actorStatementEventIDPlaceholder},
	}
}
