package server

import (
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
)

func TestArtifactAttachmentMultipartUploadAndContentHeaders(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)

	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-attach-it","display_name":"A","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("actor_id", "actor-attach-it")
	_ = mw.WriteField("refs", `["thread:thread-attach-case"]`)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="hello.txt"`)
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello-attachment"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, h.baseURL+"/artifacts/attachments", strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, string(b))
	}

	var envelope map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	art, ok := envelope["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("expected artifact object: %#v", envelope)
	}
	id, _ := art["id"].(string)
	if strings.TrimSpace(id) == "" {
		t.Fatal("missing artifact id")
	}

	contentResp, err := http.Get(h.baseURL + "/artifacts/" + id + "/content")
	if err != nil {
		t.Fatal(err)
	}
	defer contentResp.Body.Close()
	if contentResp.StatusCode != http.StatusOK {
		t.Fatalf("content status %d", contentResp.StatusCode)
	}
	ct := contentResp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type: %s", ct)
	}
	if contentResp.Header.Get("ETag") == "" {
		t.Fatal("expected ETag")
	}
	if got := contentResp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options: %q", got)
	}
	raw, err := io.ReadAll(contentResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello-attachment" {
		t.Fatalf("body %q", raw)
	}
}
