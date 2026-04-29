package server

const provenanceEventIDPlaceholder = "<event_id>"

func eventProvenance() map[string]any {
	return map[string]any{
		"sources": []string{"event:" + provenanceEventIDPlaceholder},
	}
}
