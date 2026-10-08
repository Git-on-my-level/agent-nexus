package pm

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Small fixed limits keep history reads and heartbeat writes independent of
// workspace size. No raw runner payload is accepted by this contract.
func conversationRefs(primary string, refs []string) ([]string, error) {
	if len(refs) > 8 {
		return nil, ErrInvalid
	}
	var out []string
	for _, ref := range append([]string{primary}, refs...) {
		if ref == "" {
			continue
		}
		if len(ref) > 512 || !utf8.ValidString(ref) || strings.TrimSpace(ref) != ref || !strings.Contains(ref, ":") || strings.IndexFunc(ref, unicode.IsControl) >= 0 {
			return nil, ErrInvalid
		}
		found := false
		for _, prior := range out {
			if prior == ref {
				found = true
				break
			}
		}
		if !found {
			out = append(out, ref)
		}
	}
	if len(out) > 8 {
		return nil, ErrInvalid
	}
	return out, nil
}

func sameConversationRefs(c Conversation, refs []string) bool {
	prior, err := conversationRefs(c.WorkRef, c.ContextRefs)
	if err != nil || len(prior) != len(refs) {
		return false
	}
	for i := range refs {
		if prior[i] != refs[i] {
			return false
		}
	}
	return true
}

func safeActivityText(s string, max int, required bool) bool {
	return len(s) <= max && utf8.ValidString(s) && (!required || strings.TrimSpace(s) != "") && strings.IndexFunc(s, unicode.IsControl) < 0
}

func applyTurnActivity(t *Turn, in HeartbeatInput, now time.Time) error {
	if len(in.Activity) > 50 {
		return ErrInvalid
	}
	if in.PartialResponse != nil && (in.PartialSequence <= 0 || !utf8.ValidString(*in.PartialResponse) || len(*in.PartialResponse) > t.MaxOutputBytes || strings.ContainsRune(*in.PartialResponse, 0)) {
		return ErrInvalid
	}
	if in.PartialResponse != nil && (in.PartialSequence < t.PartialSequence || (in.PartialSequence == t.PartialSequence && *in.PartialResponse != t.PartialResponse)) {
		return ErrConflict
	}
	for i, event := range in.Activity {
		if event.Sequence <= 0 || (i > 0 && event.Sequence <= in.Activity[i-1].Sequence) || (event.Kind != "status" && event.Kind != "tool") || !safeActivityText(event.Label, 120, true) || !safeActivityText(event.Target, 160, false) {
			return ErrInvalid
		}
		last := 0
		if len(t.Activity) > 0 {
			last = t.Activity[len(t.Activity)-1].Sequence
		}
		if event.Sequence <= last {
			matched := false
			for _, prior := range t.Activity {
				if prior.Sequence == event.Sequence {
					if prior.Kind != event.Kind || prior.Label != event.Label || prior.Target != event.Target {
						return ErrConflict
					}
					matched = true
					break
				}
			}
			// A retry older than retained history cannot be verified.
			if !matched {
				return ErrConflict
			}
			continue
		}
		event.RecordedAt = now
		t.Activity = append(t.Activity, event)
		if len(t.Activity) > 50 {
			t.Activity = t.Activity[len(t.Activity)-50:]
		}
	}
	if in.PartialResponse != nil {
		t.PartialResponse = *in.PartialResponse
		t.PartialSequence = in.PartialSequence
	}
	return nil
}
