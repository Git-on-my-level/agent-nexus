package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

// Exercise the production IMMEDIATE transaction admission with a shorter wait
// in the fixture, rather than making the suite wait twenty seconds per failure.
func TestHostCeremoniesWriterContention(t *testing.T) {
	requireIntegrationTest(t)
	for _, ceremony := range []string{"token", "bridge", "refresh", "workspace-grant"} {
		t.Run(ceremony, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{workspaceHumanGrantVerifier: subjectGrantVerifier{}, servingPool: func(ws *storage.Workspace) *sql.DB {
				db, err := sql.Open("sqlite", "file:"+ws.Layout().DatabasePath+"?_pragma=busy_timeout(25)&_txlock=immediate")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				return db
			}})
			agent := seedNotificationTestAgent(t, env, "worker.contention")
			var refresh string
			if ceremony == "refresh" {
				status, payload := hostHTTP(t, "POST", env.server.URL+"/auth/token", "", humanGrantBody("valid:person:seed-refresh"))
				hostStatus(t, status, http.StatusOK, payload)
				refresh = payload["tokens"].(map[string]any)["refresh_token"].(string)
			}
			held, err := env.workspace.DB().BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback()
			// Reuse the exact token proof after the refused admission. Its replay
			// marker must not be consumed until a transaction can actually start.
			signed := time.Now().UTC().Format(time.RFC3339Nano)
			request := func() (int, map[string]any) {
				if ceremony == "refresh" {
					return hostHTTP(t, "POST", env.server.URL+"/auth/token", "", map[string]any{"grant_type": "refresh_token", "refresh_token": refresh})
				}
				if ceremony == "workspace-grant" {
					return hostHTTP(t, "POST", env.server.URL+"/auth/token", "", humanGrantBody("valid:person:contended-grant"))
				}
				if ceremony == "token" {
					message := "anx-host-agent-token|" + agent.Host.ID + "|" + agent.Host.KeyID + "|worker|" + signed
					return hostHTTP(t, "POST", env.server.URL+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": agent.Host.ID, "key_id": agent.Host.KeyID, "agent_name": "worker", "signed_at": signed, "signature": hostSign(agent.Host.Private, message)})
				}
				return hostSignedHTTPAt(t, "POST", env.server.URL+"/hosts/"+agent.Host.ID+"/bridge/check-in", agent.Host.ID, agent.Host.KeyID, "bridge-check-in", agent.Host.Private, map[string]any{"instance_id": "bridge-test", "sequence": 1, "config_revision": 1, "version": "test", "agents": []any{}}, signed)
			}
			status, payload := request()
			if status != http.StatusServiceUnavailable || payload["error"].(map[string]any)["code"] != "busy" || payload["error"].(map[string]any)["recoverable"] != true {
				t.Errorf("writer contention should be retryable: status=%d payload=%v", status, payload)
			}
			if err = held.Rollback(); err != nil {
				t.Fatal(err)
			}
			status, payload = request()
			hostStatus(t, status, http.StatusOK, payload)
			// Successful assertions remain single-use.
			status, payload = request()
			hostStatus(t, status, http.StatusUnauthorized, payload)
		})
	}
}

func TestHostTokenShortWriterWaitSucceeds(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	agent := seedNotificationTestAgent(t, env, "worker.shortwait")
	held, err := env.workspace.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	released := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		released <- held.Rollback()
	}()
	signed := time.Now().UTC().Format(time.RFC3339Nano)
	message := "anx-host-agent-token|" + agent.Host.ID + "|" + agent.Host.KeyID + "|worker|" + signed
	status, payload := hostHTTP(t, "POST", env.server.URL+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": agent.Host.ID, "key_id": agent.Host.KeyID, "agent_name": "worker", "signed_at": signed, "signature": hostSign(agent.Host.Private, message)})
	hostStatus(t, status, http.StatusOK, payload)
	if err := <-released; err != nil {
		t.Fatal(err)
	}
}

func TestHostTokenInternalFailureRetainsCause(t *testing.T) {
	requireIntegrationTest(t) // Serial process-wide logger capture.
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	agent := seedNotificationTestAgent(t, env, "worker.diagnostics")
	if _, err := env.workspace.DB().Exec(`CREATE TRIGGER token_failure BEFORE INSERT ON auth_access_tokens BEGIN SELECT RAISE(ABORT,'host token probe secret=credential-secret'); END`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	signed := time.Now().UTC().Format(time.RFC3339Nano)
	message := "anx-host-agent-token|" + agent.Host.ID + "|" + agent.Host.KeyID + "|worker|" + signed
	status, payload := hostHTTP(t, "POST", env.server.URL+"/auth/token", "", map[string]any{"grant_type": "host_assertion", "host_id": agent.Host.ID, "key_id": agent.Host.KeyID, "agent_name": "worker", "signed_at": signed, "signature": hostSign(agent.Host.Private, message)})
	hostStatus(t, status, http.StatusInternalServerError, payload)
	body, _ := json.Marshal(payload)
	line := output.String()
	if strings.Count(line, "anx-core: server error") != 1 || !strings.Contains(line, "host token probe") || strings.Contains(line, "error=-") || !strings.Contains(line, "route=/auth/token") {
		t.Fatalf("lost cause: %s", line)
	}
	if strings.Contains(line, "credential-secret") || strings.Contains(string(body), "probe") || strings.Contains(string(body), "secret") {
		t.Fatalf("leaked store failure: body=%s log=%s", body, line)
	}
}
