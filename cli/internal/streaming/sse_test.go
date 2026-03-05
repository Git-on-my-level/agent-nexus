package streaming

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestReadEventParsesFrame(t *testing.T) {
	t.Parallel()
	input := strings.NewReader(": keepalive\n\nid: e-1\nevent: event\ndata: {\"ok\":true}\ndata: {\"line\":2}\n\n")
	reader := bufio.NewReader(input)

	event, err := ReadEvent(reader)
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if event.ID != "e-1" {
		t.Fatalf("unexpected id: %q", event.ID)
	}
	if event.Type != "event" {
		t.Fatalf("unexpected type: %q", event.Type)
	}
	if event.Data != "{\"ok\":true}\n{\"line\":2}" {
		t.Fatalf("unexpected data: %q", event.Data)
	}
}

func TestReadEventEOF(t *testing.T) {
	t.Parallel()
	reader := bufio.NewReader(strings.NewReader(""))
	_, err := ReadEvent(reader)
	if err == nil {
		t.Fatal("expected EOF")
	}
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}
