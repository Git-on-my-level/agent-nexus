package primitives

// EventPayloadWrapperFromBodyMap maps a full event body (as accepted by AppendEvent / API) to the
// JSON object stored in events.payload_json: an object with a required "payload" key for the
// inner payload, plus any other non-envelope keys (e.g. summary, provenance) at the wrapper root.
func EventPayloadWrapperFromBodyMap(body map[string]any) map[string]any {
	if body == nil {
		return map[string]any{"payload": map[string]any{}}
	}
	wrapper := map[string]any{}
	if raw, ok := body["payload"]; ok && raw != nil {
		p, ok := raw.(map[string]any)
		if !ok {
			wrapper["payload"] = map[string]any{}
		} else {
			inner := make(map[string]any, len(p))
			for k, v := range p {
				inner[k] = v
			}
			wrapper["payload"] = inner
		}
	} else {
		wrapper["payload"] = map[string]any{}
	}
	envelope := map[string]struct{}{
		"id": {}, "type": {}, "ts": {}, "actor_id": {}, "thread_id": {}, "refs": {}, "payload": {},
	}
	for k, v := range body {
		if _, skip := envelope[k]; skip {
			continue
		}
		switch k {
		case "archived_at", "archived_by", "trashed_at", "trashed_by", "trash_reason":
			continue
		}
		wrapper[k] = v
	}
	return wrapper
}
