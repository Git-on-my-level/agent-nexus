package router

import "strings"

// WakePacketVersion matches adapters/agent-bridge/oar_agent_bridge/models.py WAKE_PACKET_VERSION.
const WakePacketVersion = "agent-wake/v1"

type WakePacket struct {
	WakeupID             string
	Handle               string
	ActorID              string
	WorkspaceID          string
	WorkspaceName        string
	ThreadID             string
	ThreadTitle          string
	SubjectRef           string
	ResolvedSubject      map[string]any
	TriggerEventID       string
	TriggerCreatedAt     string
	TriggerAuthorActorID string
	TriggerText          string
	CurrentSummary       string
	SessionKey           string
	OARBaseURL           string
	ThreadContextURL     string
	ThreadWorkspaceURL   string
	TriggerEventURL      string
	CLIThreadInspect     string
	CLIThreadWorkspace   string
	Version              string
}

// BuildWakeRequestPayload mirrors wake packet subject fields on the durable wake request event payload.
func BuildWakeRequestPayload(
	wakeupID, handle, targetActorID, workspaceID, workspaceName, threadID string,
	triggerEventID, triggerCreatedAt, triggerText, sessionKey string,
	subjectRef string, resolvedSubject map[string]any,
) map[string]any {
	p := map[string]any{
		"wakeup_id":          wakeupID,
		"wake_artifact_id":   wakeupID,
		"target_handle":      handle,
		"target_actor_id":    targetActorID,
		"workspace_id":       workspaceID,
		"workspace_name":     workspaceName,
		"thread_id":          threadID,
		"trigger_event_id":   triggerEventID,
		"trigger_created_at": triggerCreatedAt,
		"trigger_text":       triggerText,
		"session_key":        sessionKey,
	}
	if s := strings.TrimSpace(subjectRef); s != "" {
		p["subject_ref"] = s
	}
	if len(resolvedSubject) > 0 {
		p["resolved_subject"] = resolvedSubject
	}
	return p
}

// WakeArtifactRefs orders durable refs for wake artifacts and wake request events (thread, optional subject, trigger event).
func WakeArtifactRefs(threadID, eventID, subjectRef string) []string {
	out := []string{"thread:" + threadID}
	if s := strings.TrimSpace(subjectRef); s != "" {
		out = append(out, s)
	}
	out = append(out, "event:"+eventID)
	return out
}

// ResolvedSubjectFromThread builds subject_ref and minimal resolved metadata from a thread record.
// Rule: use durable thread.subject_ref when set; otherwise leave subject empty so reply_refs fall back to thread-only context.
func ResolvedSubjectFromThread(thread map[string]any, threadID string) (subjectRef string, metadata map[string]any) {
	if thread == nil {
		return "", nil
	}
	subjectRef = strings.TrimSpace(anyString(thread["subject_ref"]))
	if subjectRef == "" {
		return "", nil
	}
	kind, _, _ := strings.Cut(subjectRef, ":")
	meta := map[string]any{
		"ref":         subjectRef,
		"subject_ref": subjectRef,
		"kind":        kind,
		"title":       firstNonEmpty(anyString(thread["title"]), threadID),
		"thread_id":   threadID,
	}
	return subjectRef, meta
}

func (p WakePacket) effectiveSubjectRef() string {
	if s := strings.TrimSpace(p.SubjectRef); s != "" {
		return s
	}
	if p.ResolvedSubject != nil {
		for _, key := range []string{"ref", "subject_ref"} {
			if s := strings.TrimSpace(anyString(p.ResolvedSubject[key])); s != "" {
				return s
			}
		}
	}
	if strings.TrimSpace(p.ThreadID) != "" {
		return "thread:" + p.ThreadID
	}
	return ""
}

func (p WakePacket) subjectContextRefs() []string {
	var refs []string
	if tid := strings.TrimSpace(p.ThreadID); tid != "" {
		refs = append(refs, "thread:"+tid)
	}
	subj := p.effectiveSubjectRef()
	if subj == "" {
		return refs
	}
	for _, existing := range refs {
		if existing == subj {
			return refs
		}
	}
	return append(refs, subj)
}

func (p WakePacket) ToContent() map[string]any {
	version := p.Version
	if version == "" {
		version = WakePacketVersion
	}
	out := map[string]any{
		"version":   version,
		"wakeup_id": p.WakeupID,
		"target": map[string]any{
			"handle":   p.Handle,
			"actor_id": p.ActorID,
		},
		"workspace": map[string]any{
			"id":   p.WorkspaceID,
			"name": p.WorkspaceName,
		},
		"thread": map[string]any{
			"id":    p.ThreadID,
			"title": p.ThreadTitle,
		},
		"trigger": map[string]any{
			"kind":             "mention",
			"message_event_id": p.TriggerEventID,
			"created_at":       p.TriggerCreatedAt,
			"author_actor_id":  p.TriggerAuthorActorID,
			"text":             p.TriggerText,
		},
		"context_inline": map[string]any{
			"current_summary": p.CurrentSummary,
		},
		"session_key": p.SessionKey,
		"context_fetch": map[string]any{
			"preferred": "threads.workspace",
			"cli":       []string{p.CLIThreadWorkspace, p.CLIThreadInspect},
			"api": map[string]any{
				"thread":        strings.TrimRight(p.OARBaseURL, "/") + "/threads/" + p.ThreadID,
				"context":       p.ThreadContextURL,
				"workspace":     p.ThreadWorkspaceURL,
				"trigger_event": p.TriggerEventURL,
			},
		},
	}
	if s := strings.TrimSpace(p.SubjectRef); s != "" {
		out["subject_ref"] = s
	}
	if len(p.ResolvedSubject) > 0 {
		out["resolved_subject"] = p.ResolvedSubject
	}
	replyRefs := append(p.subjectContextRefs(),
		"event:"+p.TriggerEventID,
		"artifact:"+p.WakeupID,
	)
	out["reply_refs"] = replyRefs
	return out
}
