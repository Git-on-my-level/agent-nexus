package stdio

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
	"github.com/Git-on-my-level/agent-nexus/mcp/protocol"
)

func TestServeWritesOnlyJSONRPCToStdout(t *testing.T) {
	cat, err := catalog.Build(catalog.CommandRegistry{Commands: []catalog.Command{
		{CommandID: "cards.list", Method: "GET", Path: "/cards"},
	}}, catalog.Policy{
		ValidClassifications: []string{catalog.ClassificationExposedRead},
		Commands: map[string]catalog.PolicyEntry{
			"cards.list": {Classification: catalog.ClassificationExposedRead},
		},
	}, catalog.BuildOptions{})
	if err != nil {
		t.Fatalf("catalog.Build() error = %v", err)
	}
	server := protocol.NewServer(cat, nil, protocol.Options{Name: "test"})
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"

	if err := Serve(context.Background(), server, strings.NewReader(input), &stdout, Options{
		Logger: log.New(&stderr, "LOG ", 0),
	}); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout lines = %d: %q", len(lines), stdout.String())
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("stdout is not JSON: %v: %q", err, lines[0])
	}
	if resp["jsonrpc"] != "2.0" || resp["id"].(float64) != 1 {
		t.Fatalf("unexpected JSON-RPC response: %#v", resp)
	}
}

func TestServeLogsTransportErrorsToStderrOnly(t *testing.T) {
	server := protocol.NewServer(nil, nil, protocol.Options{})
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := Serve(context.Background(), server, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n"), &stdout, Options{
		Logger: log.New(&stderr, "", 0),
	}); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("expected protocol error response on stdout")
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}

	if err := Serve(context.Background(), nil, strings.NewReader(""), io.Discard, Options{
		Logger: log.New(&stderr, "", 0),
	}); err == nil {
		t.Fatal("expected nil server error")
	}
}
