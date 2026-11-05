package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func personaTestApp(t *testing.T, handler http.HandlerFunc) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	a, out := dailyTestApp(t, server.URL)
	stderr := &bytes.Buffer{}
	a.Stderr = stderr
	home, err := a.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// A name this configuration already used on the host.
	raw, _ := json.Marshal(tokenCache{Token: "cached", ExpiresAt: time.Now().Add(time.Hour), Agent: map[string]any{"id": "agent-0"}})
	if err := os.WriteFile(filepath.Join(home, ".config", "anx", "hosts", "ws_test", "token-codex-spkfinal.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return a, out, stderr
}

func TestNewPersonaNearDuplicateWarnsWithoutBlocking(t *testing.T) {
	grants := 0
	a, out, stderr := personaTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			grants++
			fmt.Fprint(w, `{"agent":{"id":"agent-3","actor_id":"actor-3","handle":"codex-spkfinal-r3.host"},"registered":true,"similar_names":["codex-spkfinal","codex-spkfinal-r2"],"tokens":{"access_token":"new","expires_in":900}}`)
		case "/agents/me":
			fmt.Fprint(w, `{"agent":{"id":"agent-3"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	if exit := a.Run([]string{"--json", "--as", "codex-spkfinal-r3", "auth", "whoami"}); exit != 0 {
		t.Fatalf("registration must not be blocked: exit %d: %s %s", exit, out.String(), stderr.String())
	}
	want := "! codex-spkfinal, codex-spkfinal-r2 exist on this host. Reuse one, or name this persona for its purpose."
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr warning missing: %q", stderr.String())
	}
	payload := dailyJSON(t, out)
	found := false
	for _, raw := range asSlice(payload["warnings"]) {
		warning := asMap(raw)
		if anyString(warning["code"]) == personaNameSimilarCode {
			found = true
			if anyString(asMap(warning["details"])["name"]) != "codex-spkfinal-r3" {
				t.Fatalf("warning details: %#v", warning)
			}
		}
	}
	if !found || grants != 1 {
		t.Fatalf("JSON warning missing or extra grants (%d): %s", grants, out.String())
	}
}

func TestSingleNearDuplicateWarningAndTextMode(t *testing.T) {
	a, out, stderr := personaTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			fmt.Fprint(w, `{"agent":{"id":"agent-3"},"registered":true,"similar_names":["codex-spkfinal"],"tokens":{"access_token":"new","expires_in":900}}`)
		case "/agents/me":
			fmt.Fprint(w, `{"agent":{"id":"agent-3"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	if exit := a.Run([]string{"--as", "codex-spkfinal-r3", "auth", "whoami"}); exit != 0 {
		t.Fatalf("exit %d: %s", exit, out.String())
	}
	if !strings.Contains(stderr.String(), "! codex-spkfinal exists on this host. Reuse it, or name this persona for its purpose.") {
		t.Fatalf("stderr warning missing: %q", stderr.String())
	}
	if strings.Contains(out.String(), personaNameSimilarCode) {
		t.Fatalf("text stdout repeated the stderr warning: %s", out.String())
	}
}

func TestRegisteredWithoutSimilarNamesIsQuiet(t *testing.T) {
	a, out, stderr := personaTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			fmt.Fprint(w, `{"agent":{"id":"agent-3"},"registered":true,"similar_names":[],"tokens":{"access_token":"new","expires_in":900}}`)
		case "/agents/me":
			fmt.Fprint(w, `{"agent":{"id":"agent-3"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	if exit := a.Run([]string{"--json", "--as", "release-notes", "auth", "whoami"}); exit != 0 {
		t.Fatalf("exit %d: %s", exit, out.String())
	}
	if strings.Contains(stderr.String(), "exists on this host") || strings.Contains(out.String(), personaNameSimilarCode) {
		t.Fatalf("unexpected warning: %s %s", stderr.String(), out.String())
	}
}

func TestArchivedNameErrorNamesItAndTheHostsOtherNames(t *testing.T) {
	a, out, _ := personaTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/token" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"code":"agent_archived","message":"this agent name is archived on this host"}}`)
			return
		}
		http.NotFound(w, r)
	})
	if exit := a.Run([]string{"--json", "--as", "codex-spkfinal-r2", "auth", "whoami"}); exit == 0 {
		t.Fatalf("archived name succeeded: %s", out.String())
	}
	failure := asMap(dailyJSON(t, out)["error"])
	message := anyString(failure["message"])
	if anyString(failure["code"]) != "agent_archived" || !strings.Contains(message, `"codex-spkfinal-r2" is archived`) || !strings.Contains(message, "codex-spkfinal") || !strings.Contains(message, "restore it on the Access page") {
		t.Fatalf("archived error: %#v", failure)
	}
}

func TestUnresolvedIdentityRepairListsNamesOnThisHost(t *testing.T) {
	a, out, _ := personaTestApp(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	a.runtimeIdentity = nil
	if exit := a.Run([]string{"--json", "auth", "whoami"}); exit == 0 {
		t.Fatalf("expected unresolved identity: %s", out.String())
	}
	failure := asMap(dailyJSON(t, out)["error"])
	message := anyString(failure["message"])
	if anyString(failure["code"]) != "identity_unresolved" || !strings.Contains(message, "reuse a name already used on this host (codex-spkfinal") || !strings.Contains(message, "never invent one per task") {
		t.Fatalf("repair does not point at existing names: %#v", failure)
	}
}
