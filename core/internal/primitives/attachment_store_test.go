package primitives

import "testing"

func TestChooseAttachmentMIME(t *testing.T) {
	t.Parallel()
	m, err := ChooseAttachmentMIME("text/plain", "application/octet-stream")
	if err != nil || m != "text/plain" {
		t.Fatalf("got %q %v", m, err)
	}
	m, err = ChooseAttachmentMIME("application/octet-stream", "text/plain")
	if err != nil || m != "text/plain" {
		t.Fatalf("fallback got %q %v", m, err)
	}
	_, err = ChooseAttachmentMIME("application/octet-stream", "application/octet-stream")
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = ChooseAttachmentMIME("text/html", "text/html")
	if err == nil {
		t.Fatal("expected text/html to be rejected")
	}
}
