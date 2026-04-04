package router

import (
	"testing"
)

func TestWakePacketToContentUsesBridgeVersion(t *testing.T) {
	p := WakePacket{
		WakeupID:             "w1",
		Handle:               "bot",
		ActorID:              "a1",
		WorkspaceID:          "ws",
		WorkspaceName:        "Main",
		ThreadID:             "t1",
		ThreadTitle:          "T",
		TriggerEventID:       "e1",
		TriggerCreatedAt:     "2026-01-01T00:00:00Z",
		TriggerAuthorActorID: "u1",
		TriggerText:          "hi",
		CurrentSummary:       "s",
		SessionKey:           "sk",
		OARBaseURL:           "http://x",
		ThreadContextURL:     "http://x/threads/t1/context",
		ThreadWorkspaceURL:   "http://x/threads/t1/workspace",
		TriggerEventURL:      "http://x/events/e1",
		CLIThreadInspect:     "inspect",
		CLIThreadWorkspace:   "ws-cli",
	}
	content := p.ToContent()
	if got := content["version"]; got != WakePacketVersion {
		t.Fatalf("version: got %q want %q", got, WakePacketVersion)
	}
	refs, _ := content["reply_refs"].([]string)
	if len(refs) != 3 || refs[0] != "thread:t1" || refs[1] != "event:e1" || refs[2] != "artifact:w1" {
		t.Fatalf("reply_refs without subject: got %#v", refs)
	}
	if _, ok := content["subject_ref"]; ok {
		t.Fatalf("expected no subject_ref when empty")
	}
}

func TestWakePacketToContentIncludesSubjectAndReplyRefs(t *testing.T) {
	p := WakePacket{
		WakeupID:      "w1",
		Handle:        "bot",
		ActorID:       "a1",
		WorkspaceID:   "ws",
		WorkspaceName: "Main",
		ThreadID:      "t1",
		ThreadTitle:   "Card thread",
		SubjectRef:    "card:c1",
		ResolvedSubject: map[string]any{
			"ref": "card:c1", "kind": "card", "title": "Card thread", "thread_id": "t1",
		},
		TriggerEventID:       "e1",
		TriggerCreatedAt:     "2026-01-01T00:00:00Z",
		TriggerAuthorActorID: "u1",
		TriggerText:          "hi",
		CurrentSummary:       "s",
		SessionKey:           "sk",
		OARBaseURL:           "http://x",
		ThreadContextURL:     "http://x/threads/t1/context",
		ThreadWorkspaceURL:   "http://x/threads/t1/workspace",
		TriggerEventURL:      "http://x/events/e1",
		CLIThreadInspect:     "inspect",
		CLIThreadWorkspace:   "ws-cli",
		Version:              WakePacketVersion,
	}
	content := p.ToContent()
	if content["subject_ref"] != "card:c1" {
		t.Fatalf("subject_ref: got %#v", content["subject_ref"])
	}
	rs, _ := content["resolved_subject"].(map[string]any)
	if rs["kind"] != "card" {
		t.Fatalf("resolved_subject: got %#v", rs)
	}
	refs, _ := content["reply_refs"].([]string)
	want := []string{"thread:t1", "card:c1", "event:e1", "artifact:w1"}
	if len(refs) != len(want) {
		t.Fatalf("reply_refs len: got %v want %v", refs, want)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("reply_refs[%d]: got %q want %q", i, refs[i], want[i])
		}
	}
}

func TestResolvedSubjectFromThreadEmptyAndTopic(t *testing.T) {
	ref, meta := ResolvedSubjectFromThread(map[string]any{
		"id": "t1", "title": "Hello", "current_summary": "x",
	}, "t1")
	if ref != "" || meta != nil {
		t.Fatalf("expected empty subject, got ref=%q meta=%v", ref, meta)
	}
	ref, meta = ResolvedSubjectFromThread(map[string]any{
		"id":          "t1",
		"title":       "Topic thread",
		"subject_ref": "topic:top-1",
	}, "t1")
	if ref != "topic:top-1" {
		t.Fatalf("ref: got %q", ref)
	}
	if meta["kind"] != "topic" || meta["ref"] != "topic:top-1" {
		t.Fatalf("meta: %#v", meta)
	}
}

func TestWakeArtifactRefs(t *testing.T) {
	got := WakeArtifactRefs("t1", "e1", "")
	want := []string{"thread:t1", "event:e1"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	got2 := WakeArtifactRefs("t1", "e1", "document:d1")
	want2 := []string{"thread:t1", "document:d1", "event:e1"}
	if len(got2) != len(want2) {
		t.Fatal(got2)
	}
	for i := range want2 {
		if got2[i] != want2[i] {
			t.Fatalf("idx %d: %q vs %q", i, got2[i], want2[i])
		}
	}
}
