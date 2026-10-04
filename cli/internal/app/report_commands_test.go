package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalVisualReport = `{"kind":"anx.visual-report","schema_version":1,"title":"Release report","summary":"One bounded finding.","generated_at":"2026-10-03T06:38:37Z","projects":[{"id":"project-a","title":"Project A","summary":"Evidence is incomplete.","outcome":"Qualification unknown"}],"sources":[],"panels":[{"id":"finding","project_id":"project-a","type":"explanation","title":"Evidence needed","author":"unknown","provenance":"reported","observed_at":null,"freshness":"unavailable","source_ids":[],"data":{"text":"No observation is available."}}]}`

func TestReportSchemaAndValidateAreLocalCommands(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "report", "schema"}))
	result := asMap(payload["result"])
	if got := anyStringValue(result["kind"]); got != "anx.visual-report" {
		t.Fatalf("unexpected schema kind %q", got)
	}
	panelTypes, panelTypesOK := asStringList(result["panel_types"])
	if !panelTypesOK || len(panelTypes) != 14 || asMap(result["example"]) == nil {
		t.Fatalf("schema omitted panel types or minimal example: %#v", result)
	}
	for _, liveType := range []string{"live-initiatives", "live-asks", "live-work-mix", "live-activity"} {
		if !containsString(panelTypes, liveType) {
			t.Errorf("schema omitted shared panel type %q: %#v", liveType, panelTypes)
		}
	}

	valid := assertEnvelopeOK(t, runCLIForTest(t, home, nil, strings.NewReader(minimalVisualReport), []string{"--json", "report", "validate", "-"}))
	if asMap(valid["result"])["valid"] != true {
		t.Fatalf("expected valid report: %#v", valid)
	}

	invalid := assertEnvelopeError(t, runCLIForTest(t, home, nil, strings.NewReader(`{"kind":"anx.visual-report","schema_version":1}`), []string{"--json", "report", "validate", "-"}))
	details := asMap(asMap(invalid["error"])["details"])
	if len(asSlice(details["errors"])) == 0 {
		t.Fatalf("invalid report should return bounded diagnostics: %#v", invalid)
	}
}

func TestDocsCreateWarnsForInvalidVisualReportInTextAndJSON(t *testing.T) {
	t.Parallel()
	var receivedContentType, receivedContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/docs" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode create body: %v", err)
		}
		receivedContentType = anyStringValue(body["content_type"])
		receivedContent = anyStringValue(body["content"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"document":{"id":"doc_1","handle":"broken-report"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "broken.json")
	content := `{"kind":"anx.visual-report","schema_version":1}`
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--base-url", server.URL, "docs", "create", "--topic", "topic:launch", "--title", "Broken report", "--body-file", file}

	jsonOutput := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, append([]string{"--json"}, args...)))
	warnings := asSlice(jsonOutput["warnings"])
	if len(warnings) != 1 || anyStringValue(asMap(warnings[0])["code"]) != "invalid_visual_report" || !strings.Contains(anyStringValue(asMap(warnings[0])["message"]), "required fields") {
		t.Fatalf("expected validation warning with first errors: %#v", jsonOutput)
	}
	textOutput := runCLIForTest(t, home, nil, nil, args)
	if !strings.Contains(textOutput, "warning code=invalid_visual_report") || !strings.Contains(textOutput, "required fields") {
		t.Fatalf("expected text warning with validation details, got %q", textOutput)
	}
	if receivedContentType != "text" || receivedContent != content {
		t.Fatalf("expected text report content, got type=%q content=%q", receivedContentType, receivedContent)
	}
}

func TestReportPublishCreatesTextDocumentAndValidatesReadback(t *testing.T) {
	t.Parallel()
	var createdBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","ref":"topic:launch","thread_id":"thread_1","title":"Launch"},"documents":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			if err := json.NewDecoder(r.Body).Decode(&createdBody); err != nil {
				t.Fatalf("decode report document create: %v", err)
			}
			if got := anyStringValue(createdBody["content_type"]); got != "text" {
				t.Fatalf("expected text content type, got %q", got)
			}
			doc := asMap(createdBody["document"])
			if _, has := doc["thread_id"]; has {
				t.Fatalf("document must use its own backing thread, got %#v", doc)
			}
			if doc["subject_ref"] != "topic:topic_1" {
				t.Fatalf("expected topic subject ref, got %#v", doc)
			}
			if refs := asSlice(doc["refs"]); len(refs) != 1 || refs[0] != "topic:topic_1" {
				t.Fatalf("expected topic relation in document refs, got %#v", doc["refs"])
			}
			requestKey := anyStringValue(createdBody["request_key"])
			if requestKey == "" || requestKey != reportCreateRequestKey("topic_1", "Release report") {
				t.Fatalf("expected stable topic/title request key, got %q", requestKey)
			}
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","ref":"document:release-report","handle":"release-report","title":"Release report"},"revision":{"revision_id":"rev_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/revisions/rev_1":
			_, _ = w.Write([]byte(`{"document_id":"doc_1","revision":{"revision_id":"rev_1","content_type":"text","content":` + mustJSONString(t, minimalVisualReport) + `}}`))
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "report", "publish", file, "--topic", "topic:topic_1",
	}))
	result := asMap(payload["result"])
	if result["action"] != "created" || result["validated"] != true || result["content_type"] != "text" {
		t.Fatalf("unexpected publish result: %#v", result)
	}
	if result["doc_ref"] != "document:release-report" || anyStringValue(result["web_url"]) == "" {
		t.Fatalf("expected document ref and web URL: %#v", result)
	}
	if got := anyStringValue(createdBody["content"]); got != minimalVisualReport {
		t.Fatalf("expected serialized report string, got %q", got)
	}
}

func TestReportPublishRevisesMatchingTitleWithinTopic(t *testing.T) {
	t.Parallel()
	var revisionBody map[string]any
	var creates int
	var reads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[{"id":"doc_1","ref":"document:fleet-dashboard","handle":"fleet-dashboard","title":"Fleet Dashboard","state":"active"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			creates++
			_, _ = w.Write([]byte(`{"document":{"id":"doc_2"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1":
			reads++
			if reads == 1 {
				_, _ = w.Write([]byte(`{"document":{"id":"doc_1","ref":"document:fleet-dashboard","handle":"fleet-dashboard","title":"Fleet Dashboard","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","content_type":"text","content":` + mustJSONString(t, minimalVisualReport) + `}}`))
				return
			}
		case r.Method == http.MethodPost && r.URL.Path == "/docs/doc_1/revisions":
			if err := json.NewDecoder(r.Body).Decode(&revisionBody); err != nil {
				t.Fatalf("decode report revision: %v", err)
			}
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/revisions/rev_2":
			_, _ = w.Write([]byte(`{"document_id":"doc_1","revision":{"revision_id":"rev_2","content_type":"text","content":` + mustJSONString(t, minimalVisualReport) + `}}`))
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-report", `{"agent":"agent-report","actor_id":"actor-report","access_token":"token-report","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-report", "report", "publish", file, "--topic", "topic:topic_1", "--title", "Fleet Dashboard",
	}))
	result := asMap(payload["result"])
	if result["action"] != "revised" || result["doc_ref"] != "document:fleet-dashboard" || creates != 0 {
		t.Fatalf("expected idempotent direct revision, result=%#v creates=%d", result, creates)
	}
	if revisionBody["if_base_revision"] != "rev_1" || revisionBody["content_type"] != "text" || revisionBody["content"] != minimalVisualReport {
		t.Fatalf("unexpected report revision body: %#v", revisionBody)
	}
}

func TestReportPublishExplicitDocDoesNotFuzzyMatch(t *testing.T) {
	t.Parallel()
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[{"id":"doc_1","ref":"document:fleet-dashboard","handle":"fleet-dashboard","title":"Fleet Dashboard","state":"active"}]}`))
		case r.Method == http.MethodPost:
			writes++
			t.Errorf("unexpected write while resolving an inexact --doc")
			http.Error(w, "unexpected write", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	response := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "report", "publish", file, "--topic", "topic:topic_1", "--doc", "Fleet Dash",
	}))
	if anyStringValue(asMap(response["error"])["code"]) != "document_not_in_topic" || writes != 0 {
		t.Fatalf("expected exact --doc lookup without writes: response=%#v writes=%d", response, writes)
	}
}

func TestReportPublishExplicitArchivedDocRequiresUnarchive(t *testing.T) {
	t.Parallel()
	var reads, writes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[{"id":"doc_1","ref":"document:release-report","handle":"release-report","title":"Release report","state":"archived"}]}`))
		case r.Method == http.MethodGet || r.Method == http.MethodPost:
			if r.Method == http.MethodGet {
				reads++
			} else {
				writes++
			}
			t.Errorf("archived explicit target must be rejected before read/write: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	response := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "report", "publish", file, "--topic", "topic:topic_1", "--doc", "document:release-report",
	}))
	err := asMap(response["error"])
	command := "anx docs unarchive document:release-report"
	details := asMap(err["details"])
	if anyStringValue(err["code"]) != "archived_report_document" ||
		!strings.Contains(anyStringValue(err["message"]), command) ||
		!strings.Contains(anyStringValue(details["hint"]), command) || reads != 0 || writes != 0 {
		t.Fatalf("expected explicit archived-doc repair without read/write: response=%#v reads=%d writes=%d", response, reads, writes)
	}
	actions := asSlice(err["next_actions"])
	if len(actions) != 1 {
		t.Fatalf("expected exact unarchive repair action %q, got %#v", command, actions)
	}
	argv := asSlice(asMap(actions[0])["argv"])
	argvText := make([]string, 0, len(argv))
	for _, arg := range argv {
		argvText = append(argvText, anyStringValue(arg))
	}
	if strings.Join(argvText, " ") != command {
		t.Fatalf("expected exact unarchive repair action %q, got %#v", command, actions)
	}
}

func TestFindAutomaticReportDocumentsOnlyReturnsActiveMatches(t *testing.T) {
	docs := []any{
		map[string]any{"id": "archived", "title": "Release report", "state": "archived"},
		map[string]any{"id": "trashed", "title": "Release report", "state": "trashed"},
		map[string]any{"id": "active", "title": "Release report", "state": "active"},
		map[string]any{"id": "unknown", "title": "Release report"},
	}
	matches := findAutomaticReportDocuments(docs, "Release report")
	if len(matches) != 1 || reportDocumentID(matches[0]) != "active" {
		t.Fatalf("expected only active matching reports, got %#v", matches)
	}
}

func TestReportPublishRequiresReplaceForExplicitProseDoc(t *testing.T) {
	t.Parallel()
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[{"id":"doc_1","ref":"document:notes","handle":"notes","title":"Notes","state":"active"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1":
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","ref":"document:notes","handle":"notes","title":"Notes","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","content_type":"text","content":"plain prose"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs/doc_1/revisions":
			writes++
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1"},"revision":{"revision_id":"rev_2"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/revisions/rev_2":
			_, _ = w.Write([]byte(`{"revision":{"revision_id":"rev_2","content_type":"text","content":` + mustJSONString(t, minimalVisualReport) + `}}`))
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDerivedAgentFixture(t, home, "agent-report", `{"agent":"agent-report","actor_id":"actor-report","access_token":"token-report","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	response := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-report", "report", "publish", file, "--topic", "topic:topic_1", "--doc", "doc:notes",
	}))
	if anyStringValue(asMap(response["error"])["code"]) != "replace_required" || writes != 0 {
		t.Fatalf("expected --replace guard without writes: response=%#v writes=%d", response, writes)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-report", "report", "publish", file, "--topic", "topic:topic_1", "--doc", "doc:notes", "--replace",
	}))
	if asMap(payload["result"])["action"] != "revised" || writes != 1 {
		t.Fatalf("expected --replace to allow explicit prose replacement: result=%#v writes=%d", payload["result"], writes)
	}
}

func TestReportPublishAutomaticMatchSkipsProseDocument(t *testing.T) {
	t.Parallel()
	var createBody map[string]any
	var revisions int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[{"id":"doc_notes","ref":"document:release-report","handle":"release-report","title":"Release report","state":"active"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_notes":
			_, _ = w.Write([]byte(`{"document":{"id":"doc_notes","ref":"document:release-report","handle":"release-report","title":"Release report"},"revision":{"revision_id":"rev_notes","content_type":"text","content":"plain prose"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			_, _ = w.Write([]byte(`{"document":{"id":"doc_report","ref":"document:release-report-2","handle":"release-report-2","title":"Release report"},"revision":{"revision_id":"rev_report"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_report/revisions/rev_report":
			_, _ = w.Write([]byte(`{"revision":{"revision_id":"rev_report","content_type":"text","content":` + mustJSONString(t, minimalVisualReport) + `}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/revisions"):
			revisions++
			t.Errorf("automatic matching must not revise prose")
			http.Error(w, "unexpected revision", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "report", "publish", file, "--topic", "topic:topic_1",
	}))
	createdDoc := asMap(createBody["document"])
	if asMap(payload["result"])["action"] != "created" || createdDoc["thread_id"] != nil || createdDoc["subject_ref"] != "topic:topic_1" || revisions != 0 {
		t.Fatalf("expected separate document thread with topic relation and no prose revision: result=%#v body=%#v revisions=%d", payload["result"], createBody, revisions)
	}
}

func TestReportPublishRejectsReadbackThatDoesNotMatchWrittenRevision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, revisionID, contentType, content string
	}{
		{name: "revision id", revisionID: "rev_other", contentType: "text", content: minimalVisualReport},
		{name: "content type", revisionID: "rev_written", contentType: "structured", content: minimalVisualReport},
		{name: "content bytes", revisionID: "rev_written", contentType: "text", content: minimalVisualReport + " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
					_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[]}`))
				case r.Method == http.MethodPost && r.URL.Path == "/docs":
					_, _ = w.Write([]byte(`{"document":{"id":"doc_1","ref":"document:release-report"},"revision":{"revision_id":"rev_written"}}`))
				case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/revisions/rev_written":
					_, _ = w.Write([]byte(`{"revision":{"revision_id":` + mustJSONString(t, tc.revisionID) + `,"content_type":` + mustJSONString(t, tc.contentType) + `,"content":` + mustJSONString(t, tc.content) + `}}`))
				default:
					t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			home := t.TempDir()
			file := filepath.Join(home, "report.json")
			if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
				t.Fatal(err)
			}
			response := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{
				"--json", "--base-url", server.URL, "report", "publish", file, "--topic", "topic:topic_1",
			}))
			if anyStringValue(asMap(response["error"])["code"]) != "publish_readback_failed" {
				t.Fatalf("expected strict written-revision verification, got %#v", response)
			}
		})
	}
}

func TestReportPublishRejectsAmbiguousAutomaticReportMatch(t *testing.T) {
	t.Parallel()
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic_1/workspace":
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_1","thread_id":"thread_1"},"documents":[{"id":"doc_1","title":"Release report","state":"active"},{"id":"doc_2","title":"Release report","state":"active"}]}`))
		case r.Method == http.MethodGet && (r.URL.Path == "/docs/doc_1" || r.URL.Path == "/docs/doc_2"):
			docID := strings.TrimPrefix(r.URL.Path, "/docs/")
			_, _ = w.Write([]byte(`{"document":{"id":` + mustJSONString(t, docID) + `,"title":"Release report"},"revision":{"revision_id":"rev_1","content_type":"text","content":` + mustJSONString(t, minimalVisualReport) + `}}`))
		case r.Method == http.MethodPost:
			writes++
			t.Errorf("ambiguous automatic match must not write")
			http.Error(w, "unexpected write", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected report publish request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	file := filepath.Join(home, "report.json")
	if err := os.WriteFile(file, []byte(minimalVisualReport), 0o600); err != nil {
		t.Fatal(err)
	}
	response := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "report", "publish", file, "--topic", "topic:topic_1",
	}))
	if anyStringValue(asMap(response["error"])["code"]) != "ambiguous_report_document" || writes != 0 {
		t.Fatalf("expected ambiguous report match error without writes: response=%#v writes=%d", response, writes)
	}
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestDocsRevisionProposalReportsUnappliedState(t *testing.T) {
	result := proposalPreviewResult(
		"docs.revisions.create", "POST", "/docs/doc_1/revisions", map[string]string{"document_id": "doc_1"},
		map[string]any{"content_type": "text", "content": "new content"}, "draft-1", "proposal.json", "diff", "anx docs revise --apply --proposal-id draft-1",
	)
	data := asMap(result.Data)
	if data["applied"] != false || data["head_unchanged"] != true || !strings.Contains(anyStringValue(data["message"]), "head is unchanged") {
		t.Fatalf("proposal result did not state that the head remains unchanged: %#v", data)
	}
	if !strings.Contains(result.Text, "head is unchanged") {
		t.Fatalf("text result did not state that the head remains unchanged: %q", result.Text)
	}
	if actions := deriveNextActions("docs revise", nil, data); len(actions) != 1 || strings.Join(actions[0].Argv, " ") != "anx docs revise --apply --proposal-id draft-1" {
		t.Fatalf("expected explicit apply next action, got %#v", actions)
	}
}
