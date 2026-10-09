package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestResourceAccessNULJSONHTTP(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "nul-owner", "nul-owner-actor", "nul-owner", "nul-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "nul-stranger", "nul-stranger-actor", "nul-stranger", "nul-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "nul-agent", "nul-agent-actor", "nul.agent", "nul-agent-token")
	private, _, err := s.CreateDocument(ctx, owner.ActorID, map[string]any{"id": "[]", "title": "private"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{owner.AccessToken, stranger.AccessToken, agent.AccessToken} {
		for _, content := range []any{map[string]any{"text": "prose\x00document:[]"}, map[string]any{"key\x00document:[]": "value"}, map[string]any{"nested": `{"text":"prose\u0000document:[]"}`}} {
			resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/docs", map[string]any{"document": map[string]any{"title": "NULRejected"}, "content_type": "structured", "content": content, "refs": []string{}}, token, 400)
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if body["error"].(map[string]any)["code"] != "invalid_request" {
				t.Fatalf("wrong error shape: %#v", body)
			}
		}
	}
	// Generic event payloads use the same rejection boundary, including owners.
	postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{"event": map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "prose\x00document:[]"}}}, owner.AccessToken, 400).Body.Close()
	// Ordinary structured content remains readable; a valid private reference
	// still inherits ownership rather than being rejected as invalid text.
	for _, text := range []string{"ordinary public text", "prose document:[]"} {
		resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/docs", map[string]any{"document": map[string]any{"title": "control"}, "content_type": "structured", "content": map[string]any{"text": text}, "refs": []string{}}, owner.AccessToken, 201)
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		id := body["document"].(map[string]any)["id"].(string)
		for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
			want := 200
			if text == "prose document:[]" {
				want = 404
			}
			getJSONExpectStatusWithAuth(t, env.server.URL+"/docs/"+id, token, want).Body.Close()
		}
	}
	// Binary is never reclassified as text merely because its bytes parse as
	// JSON. Both raw and JSON-escaped controls still participate in ownership.
	for _, content := range []string{`{"text":"\u0000"}`, "prose\x00document:[]\x00", `{"text":"prose\u0000document:[]\u0000"}`} {
		private := content != `{"text":"\u0000"}`
		create := map[string]any{"document": map[string]any{"title": "binary control"}, "content_type": "binary", "content_base64": base64.StdEncoding.EncodeToString([]byte(content)), "refs": []string{}}
		response := postJSONExpectStatusWithAuth(t, env.server.URL+"/docs", create, owner.AccessToken, 201)
		var saved map[string]any
		if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		id := saved["document"].(map[string]any)["id"].(string)
		if saved["revision"].(map[string]any)["content_base64"] != create["content_base64"] {
			t.Fatal("binary document bytes changed")
		}
		artifact := postNULBinaryAttachment(t, env.server.URL, owner.AccessToken, content, 201)
		for _, token := range []string{owner.AccessToken, stranger.AccessToken, agent.AccessToken} {
			readStatus, writeStatus := 200, 201
			if private && token != owner.AccessToken {
				readStatus, writeStatus = 404, 404
			}
			getJSONExpectStatusWithAuth(t, env.server.URL+"/docs/"+id, token, readStatus).Body.Close()
			read := getJSONExpectStatusWithAuth(t, env.server.URL+"/artifacts/"+artifact+"/content", token, readStatus)
			body, err := io.ReadAll(read.Body)
			read.Body.Close()
			if err != nil || readStatus == 200 && !bytes.Equal(body, []byte(content)) {
				t.Fatalf("binary attachment read changed: %q %v", body, err)
			}
			postJSONExpectStatusWithAuth(t, env.server.URL+"/docs", create, token, writeStatus).Body.Close()
			postNULBinaryAttachment(t, env.server.URL, token, content, writeStatus)
		}
	}
}

func postNULBinaryAttachment(t *testing.T, base, token, content string, want int) string {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("refs", "[]"); err != nil {
		t.Fatal(err)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="binary.png"`)
	h.Set("Content-Type", "image/png")
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/artifacts/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != want {
		t.Fatalf("attachment status=%d want %d body=%s err=%v", resp.StatusCode, want, raw, err)
	}
	if want != 201 {
		return ""
	}
	var saved struct {
		Artifact map[string]any `json:"artifact"`
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	return saved.Artifact["id"].(string)
}
