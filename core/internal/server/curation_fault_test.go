package server

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCurationMetricsLogsSQLErrorCause(t *testing.T) {
	requireIntegrationTest(t) // Serial log capture around the actual HTTP/storage path.
	f := newCurationFixture(t, "metrics-fault")
	if _, err := f.env.workspace.DB().Exec(`DROP INDEX idx_brief_notes_next_at`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	request, err := http.NewRequest(http.MethodGet, f.env.server.URL+"/curation/metrics?token=request-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.human.AccessToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := new(bytes.Buffer)
	body.ReadFrom(response.Body)
	if response.StatusCode != http.StatusInternalServerError || !strings.Contains(body.String(), "work operation failed") {
		t.Fatal(response.StatusCode, body.String())
	}
	line := output.String()
	if strings.Count(line, "anx-core: server error") != 1 || strings.Contains(line, "error_fingerprint=") || !strings.Contains(line, "route=/curation/metrics") || !strings.Contains(line, "no such index") || !strings.Contains(line, "idx_brief_notes_next_at") {
		t.Fatal(line)
	}
	id := response.Header.Get("X-Request-ID")
	if uuid.Validate(id) != nil || !strings.Contains(line, "request_id="+id) {
		t.Fatal(id, line)
	}
	for _, secret := range []string{"request-secret", f.human.AccessToken} {
		if strings.Contains(line+body.String(), secret) {
			t.Fatalf("secret leaked: %s %s", line, body.String())
		}
	}
	if strings.Contains(body.String(), "idx_brief") || strings.Contains(body.String(), "SQL logic") {
		t.Fatal(body.String())
	}
}
