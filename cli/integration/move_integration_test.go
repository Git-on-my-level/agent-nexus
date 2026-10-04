//go:build integration

package integration

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests run the compiled CLI against two independent anx-core processes
// and SQLite workspaces. The proxy only injects the requested failure; every
// read and mutation is still handled by a real core.
func TestMoveCardRealCoresUsesCanonicalPlanAndRewritesSelfRefs(t *testing.T) {
	pair := newMoveCorePair(t)
	card := pair.createCard(t, "source-card", map[string]any{"authority": "nexus"}, "")
	sourceCard := pair.source.getCore(t, "/cards/"+url.PathEscape(card))["card"]
	sourceUUIDRef := "card:" + moveIntegrationString(integrationMap(sourceCard)["id"])
	plan := map[string]any{"steps": []any{
		map[string]any{"id": "self-review", "title": "Review this card", "ref": card},
		map[string]any{"id": "uuid-self-review", "title": "Review this card by UUID", "ref": sourceUUIDRef},
	}}
	pair.source.runCLIExpectOK(t, "move-agent", plan, "plan", "set", card, "--from-file", "-")

	result := pair.source.runCLIExpectOK(t, "move-agent", nil, "move", "card", card, "--to", "destination")
	destinationRef := mustStringPath(t, result.Payload, "result.destination_ref")
	destinationPlan := pair.destination.runCLIExpectOK(t, "move-agent", nil, "plan", "show", destinationRef)
	refs, ok := getPathValue(destinationPlan.Payload, "result.plan.steps")
	if !ok {
		t.Fatalf("destination canonical plan missing: %s", destinationPlan.Stdout)
	}
	steps, ok := refs.([]any)
	wantSelfRef := "card:" + moveIntegrationDeterministicUUID(mustStringPath(t, result.Payload, "result.move_id"), "card", card)
	stepRef := ""
	if ok && len(steps) > 0 {
		stepRef = moveIntegrationString(integrationMap(steps[0])["ref"])
	}
	if !ok || len(steps) != 2 || stepRef != wantSelfRef || moveIntegrationString(integrationMap(steps[1])["ref"]) != wantSelfRef {
		t.Fatalf("destination plan did not rewrite handle and UUID self-references: destination_ref=%q expected_step=%q steps=%#v response=%s", destinationRef, wantSelfRef, steps, destinationPlan.Stdout)
	}
	sourceWork := pair.source.getCore(t, "/work/"+url.PathEscape(card))
	if moveIntegrationState(sourceWork, "work") != "archived" {
		t.Fatalf("source card was not tombstoned after canonical plan verification: %#v", sourceWork)
	}
}

func TestMoveTopicRealCoresPreservesSharedBoardAndRewritesPlanRefs(t *testing.T) {
	pair := newMoveCorePair(t)
	fixture := pair.createTopicFixture(t, "shared-board")

	preview := pair.source.runCLIExpectOK(t, "move-agent", nil, "move", "topic", fixture.topic, "--to", "destination", "--dry-run")
	actions, ok := getPathValue(preview.Payload, "result.actions")
	if !ok {
		t.Fatalf("dry run omitted actions: %s", preview.Stdout)
	}
	rows, _ := actions.([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["source_ref"] == fixture.unrelatedCard || row["kind"] == "board" && row["action"] != "preserve" {
			t.Fatalf("dry run included a contextual shared resource in the move set: %#v", row)
		}
	}

	result := pair.source.runCLIExpectOK(t, "move-agent", nil, "move", "topic", fixture.topic, "--to", "destination")
	destinationTopicRef := mustStringPath(t, result.Payload, "result.destination_ref")
	destinationTopic := pair.destination.getCore(t, "/topics/"+url.PathEscape(destinationTopicRef))
	topic, _ := destinationTopic["topic"].(map[string]any)
	if len(integrationStrings(topic["board_refs"])) != 0 {
		t.Fatalf("destination topic retained a source-workspace board ref: %#v", topic["board_refs"])
	}
	cardRefs := integrationStrings(topic["related_refs"])
	moveID := mustStringPath(t, result.Payload, "result.move_id")
	wantCardARef := "card:" + moveIntegrationDeterministicUUID(moveID, "card", fixture.cardA)
	wantCardBRef := "card:" + moveIntegrationDeterministicUUID(moveID, "card", fixture.cardB)
	if len(cardRefs) != 2 || integrationContains(cardRefs, fixture.cardA) || integrationContains(cardRefs, fixture.cardB) {
		t.Fatalf("topic card refs were not rewritten: %#v", topic["related_refs"])
	}
	wantPinnedDocumentRef := "document:" + moveIntegrationDeterministicUUID(moveID, "document", fixture.pinnedDocument)
	wantPinnedDocumentCardRef := "card:" + moveIntegrationDeterministicUUID(moveID, "card", fixture.cardB)
	destinationPlan := pair.destination.runCLIExpectOK(t, "move-agent", nil, "plan", "show", wantCardARef)
	stepsRaw, _ := getPathValue(destinationPlan.Payload, "result.plan.steps")
	steps, _ := stepsRaw.([]any)
	if len(steps) != 2 || moveIntegrationString(integrationMap(steps[0])["ref"]) != wantCardBRef || moveIntegrationString(integrationMap(steps[1])["ref"]) != wantCardBRef {
		t.Fatalf("plan link was not rewritten across the moved set: %s", destinationPlan.Stdout)
	}
	destinationCardA := integrationMap(pair.destination.getCore(t, "/work/"+url.PathEscape(wantCardARef))["work"])
	destinationCardB := integrationMap(pair.destination.getCore(t, "/work/"+url.PathEscape(wantCardBRef))["work"])
	destinationPinnedByID := pair.destination.getCore(t, "/docs/"+url.PathEscape(wantPinnedDocumentRef))
	destinationPinnedRef := moveIntegrationString(destinationCardB["document_ref"])
	if destinationPinnedRef == "" {
		t.Fatalf("destination card lost its pinned document ref: %#v", destinationCardB)
	}
	destinationPinnedByCardRef := pair.destination.getCore(t, "/docs/"+url.PathEscape(destinationPinnedRef))
	destinationPinnedID := moveIntegrationString(integrationMap(destinationPinnedByID["document"])["id"])
	sourcePinnedID := moveIntegrationString(integrationMap(pair.source.getCore(t, "/docs/"+url.PathEscape(fixture.pinnedDocument))["document"])["id"])
	if destinationPinnedID != moveIntegrationString(integrationMap(destinationPinnedByCardRef["document"])["id"]) || destinationPinnedID != strings.TrimPrefix(wantPinnedDocumentRef, "document:") || destinationPinnedID == sourcePinnedID {
		t.Fatalf("card-attached document was not included and linked in destination: %#v", destinationCardB)
	}
	sourceCardBUUIDRef := "card:" + fixture.cardBID
	if refs := integrationStrings(destinationCardA["related_refs"]); countIntegrationRef(refs, wantCardBRef) != 1 || integrationContains(refs, fixture.cardB) || integrationContains(refs, sourceCardBUUIDRef) {
		t.Fatalf("card-related handle and UUID aliases were not rewritten and deduplicated: %#v", destinationCardA["related_refs"])
	}
	wantAliasDocumentRef := "document:" + moveIntegrationDeterministicUUID(moveID, "document", fixture.aliasDocument)
	if !integrationContains(integrationStrings(destinationCardA["related_refs"]), wantAliasDocumentRef) || integrationContains(integrationStrings(destinationCardA["related_refs"]), fixture.aliasDocument) {
		t.Fatalf("card-related document ref was not rewritten: %#v", destinationCardA["related_refs"])
	}
	destinationDocRefs := integrationStrings(topic["document_refs"])
	if len(destinationDocRefs) != 1 || destinationDocRefs[0] == fixture.document {
		t.Fatalf("document link was not rewritten: %#v", topic["document_refs"])
	}
	destinationDocRef := destinationDocRefs[0]
	destinationDoc := pair.destination.getCore(t, "/docs/"+url.PathEscape(destinationDocRef))
	document, _ := destinationDoc["document"].(map[string]any)
	revision, _ := destinationDoc["revision"].(map[string]any)
	if moveIntegrationString(document["title"]) == "" || revision["content"] != "Synthetic topic move document." {
		t.Fatalf("destination document content or metadata was not preserved: %#v", destinationDoc)
	}
	if _, status := pair.destination.getCoreStatus(t, "/docs/"+url.PathEscape(wantPinnedDocumentRef)); status != http.StatusOK {
		t.Fatalf("pinned document was not copied to destination: status=%d", status)
	}
	aliasDocument := pair.destination.getCore(t, "/docs/"+url.PathEscape(wantAliasDocumentRef))
	aliasRevision := integrationMap(aliasDocument["revision"])
	aliasRefs := integrationStrings(aliasRevision["refs"])
	destinationCardBIdentity := integrationMap(pair.destination.getCore(t, "/cards/"+url.PathEscape(wantCardBRef))["card"])
	resolvedAliasCardIdentity := map[string]any{}
	aliasCardRefs := []string{}
	for _, ref := range aliasRefs {
		if strings.HasPrefix(ref, "card:") {
			aliasCardRefs = append(aliasCardRefs, ref)
		}
	}
	if len(aliasCardRefs) == 1 {
		resolvedAliasCardIdentity = integrationMap(pair.destination.getCore(t, "/cards/"+url.PathEscape(aliasCardRefs[0]))["card"])
	}
	if len(aliasCardRefs) != 1 || moveIntegrationString(resolvedAliasCardIdentity["id"]) != moveIntegrationString(destinationCardBIdentity["id"]) || moveIntegrationString(destinationCardBIdentity["id"]) != strings.TrimPrefix(wantPinnedDocumentCardRef, "card:") || moveIntegrationString(destinationCardBIdentity["id"]) == fixture.cardBID {
		t.Fatalf("document refs did not rewrite handle and UUID aliases: %#v", aliasRevision["refs"])
	}
	pinnedListed := false
	aliasListed := false
	for _, raw := range rows {
		row := integrationMap(raw)
		if row["kind"] == "document" && row["source_ref"] == fixture.pinnedDocument {
			pinnedListed = true
		}
		if row["kind"] == "document" && row["source_ref"] == fixture.aliasDocument {
			aliasListed = true
		}
	}
	if !pinnedListed {
		t.Fatalf("dry run omitted the document pinned to a selected card: %s", preview.Stdout)
	}
	if !aliasListed {
		t.Fatalf("dry run omitted the document linked from a selected card: %s", preview.Stdout)
	}
	if got := moveIntegrationState(pair.source.getCore(t, "/boards/"+url.PathEscape(fixture.board)), "board"); got != "active" {
		t.Fatalf("shared source board was changed: state=%q", got)
	}
	if got := moveIntegrationState(pair.source.getCore(t, "/work/"+url.PathEscape(fixture.unrelatedCard)), "work"); got != "active" {
		t.Fatalf("unrelated card on shared board was changed: state=%q", got)
	}
	if _, status := pair.destination.getCoreStatus(t, "/work/"+url.PathEscape(fixture.unrelatedCard)); status != http.StatusNotFound {
		t.Fatalf("unrelated shared-board card was copied: status=%d", status)
	}
}

func TestMoveTopicRealCoresResumesFromEveryJournalPhase(t *testing.T) {
	pair := newMoveCorePair(t)
	phases := []string{"snapshot", "create_destination", "refs_rewritten", "verified", "source_transition", "complete"}
	for index, phase := range phases {
		t.Run(phase, func(t *testing.T) {
			fixture := pair.createTopicFixture(t, fmt.Sprintf("resume-%d", index))
			rule := moveIntegrationPhaseRule(phase)
			fault := pair.destinationFault
			if rule.source {
				fault = pair.sourceFault
			}
			fault.arm(rule)
			first := pair.source.runCLI(t, "move-agent", nil, "move", "topic", fixture.topic, "--to", "destination")
			if first.ExitCode == 0 || !fault.wasFired() {
				t.Fatalf("injected %s failure did not interrupt the first move: exit=%d stdout=%s stderr=%s", phase, first.ExitCode, first.Stdout, first.Stderr)
			}
			resumed := pair.source.runCLIExpectOK(t, "move-agent", nil, "move", "topic", fixture.topic, "--to", "destination")
			if mustStringPath(t, resumed.Payload, "result.status") != "moved" {
				t.Fatalf("resume from %s did not complete: %s", phase, resumed.Stdout)
			}
			if got := moveIntegrationState(pair.source.getCore(t, "/topics/"+url.PathEscape(fixture.topic)), "topic"); got != "archived" {
				t.Fatalf("resume from %s left source topic active: state=%q", phase, got)
			}
		})
	}
}

func TestMoveTopicRealCoreDryRunListsInaccessibleLinkedDocument(t *testing.T) {
	pair := newMoveCorePair(t)
	topic := pair.createTopic(t, "missing-doc")
	pair.source.runCLIExpectOK(t, "move-agent", map[string]any{"patch": map[string]any{
		"document_refs": []string{"document:missing-move-doc"},
	}}, "topics", "patch", "--topic-id", topic)
	preview := pair.source.runCLIExpectOK(t, "move-agent", nil, "move", "topic", topic, "--to", "destination", "--dry-run")
	actionsRaw, _ := getPathValue(preview.Payload, "result.actions")
	listed := false
	for _, raw := range integrationSlice(actionsRaw) {
		row, _ := raw.(map[string]any)
		if row["action"] == "fail" && row["kind"] == "document" && row["source_ref"] == "document:missing-move-doc" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("dry run did not explicitly list the inaccessible document: %s", preview.Stdout)
	}
	failed := pair.source.runCLI(t, "move-agent", nil, "move", "topic", topic, "--to", "destination")
	if failed.ExitCode == 0 || !strings.Contains(failed.Stdout, "move_source_unavailable") {
		t.Fatalf("move did not fail explicitly for its inaccessible linked document: %s", failed.Stdout)
	}
	if got := moveIntegrationState(pair.source.getCore(t, "/topics/"+url.PathEscape(topic)), "topic"); got != "active" {
		t.Fatalf("dry run changed the source topic: %q", got)
	}
}

func TestMoveSourceBackedCardRealCoreBindsDestinationConnection(t *testing.T) {
	pair := newMoveCorePair(t)
	card := pair.createCard(t, "source-backed-mapped", map[string]any{
		"authority": "github", "connection_id": "source-connection", "native_id": "issue-604",
	}, "")
	result := pair.source.runCLIExpectOK(t, "move-agent", nil, "move", "card", card, "--to", "destination", "--connection-map", "source-connection=destination-connection")
	destinationRef := mustStringPath(t, result.Payload, "result.destination_ref")
	workResponse := pair.destination.getCore(t, "/work/"+url.PathEscape(destinationRef))
	source := integrationMap(integrationMap(workResponse["work"])["source"])
	if moveIntegrationString(source["authority"]) != "github" || moveIntegrationString(source["connection_id"]) != "destination-connection" || moveIntegrationString(source["native_id"]) != "issue-604" {
		t.Fatalf("destination card did not bind the destination-local connection mapping: %#v", source)
	}
	if state := moveIntegrationState(pair.source.getCore(t, "/work/"+url.PathEscape(card)), "work"); state != "archived" {
		t.Fatalf("source-backed card was not archived after resync: %q", state)
	}
}

func TestMoveCardRealCoreRejectsConflictingIdentityAndMissingDestinationGrant(t *testing.T) {
	t.Run("conflicting destination identity", func(t *testing.T) {
		pair := newMoveCorePair(t)
		sourceCard := pair.createCard(t, "source-backed", map[string]any{"authority": "github", "connection_id": "source-connection", "native_id": "issue-603"}, "")
		pair.destination.runCLIExpectOK(t, "move-agent", map[string]any{
			"title":  "Existing destination source",
			"source": map[string]any{"authority": "github", "connection_id": "destination-connection", "native_id": "issue-603"},
		}, "work", "create", "--from-file", "-")
		result := pair.source.runCLI(t, "move-agent", nil, "move", "card", sourceCard, "--to", "destination", "--connection-map", "source-connection=destination-connection")
		if result.ExitCode == 0 || !strings.Contains(result.Stdout, "destination_source_identity_conflict") {
			t.Fatalf("conflicting mapped identity was not rejected: %s", result.Stdout)
		}
		work := pair.source.getCore(t, "/work/"+url.PathEscape(sourceCard))
		if moveIntegrationState(work, "work") != "active" {
			t.Fatalf("identity conflict changed the source: %#v", work)
		}
	})

	t.Run("missing destination host grant", func(t *testing.T) {
		pair := newMoveCorePair(t)
		sourceCard := pair.createCard(t, "missing-grant", map[string]any{"authority": "nexus"}, "")
		removeMoveDestinationCredential(t, pair)
		result := pair.source.runCLI(t, "move-agent", nil, "move", "card", sourceCard, "--to", "destination")
		if result.ExitCode == 0 {
			t.Fatalf("move unexpectedly proceeded without a destination-local host grant: %s", result.Stdout)
		}
		work := pair.source.getCore(t, "/work/"+url.PathEscape(sourceCard))
		sourceWork := integrationMap(work["work"])
		if moveIntegrationState(work, "work") != "active" || sourceWork["workspace_move"] != nil {
			t.Fatalf("missing destination grant changed the source: %#v", work)
		}
	})
}

func TestMoveCardRealCoreFencesSourceEditMidTransfer(t *testing.T) {
	pair := newMoveCorePair(t)
	card := pair.createCard(t, "edited-during-transfer", map[string]any{"authority": "nexus"}, "")
	pair.destinationFault.arm(moveIntegrationFaultRule{
		method: http.MethodPost, path: "/work", after: true,
		afterHook: func() {
			pair.source.runCLIExpectOK(t, "move-agent", nil, "cards", "revise", card, "--title", "Edited during transfer")
		},
	})
	first := pair.source.runCLI(t, "move-agent", nil, "move", "card", card, "--to", "destination")
	if first.ExitCode == 0 || !pair.destinationFault.wasFired() {
		t.Fatalf("injected post-create source edit did not interrupt the transfer: %s", first.Stdout)
	}
	resume := pair.source.runCLI(t, "move-agent", nil, "move", "card", card, "--to", "destination")
	if resume.ExitCode == 0 || !strings.Contains(resume.Stdout, "source_changed") {
		t.Fatalf("source edit was not reported on resume: %s", resume.Stdout)
	}
	if state := moveIntegrationState(pair.source.getCore(t, "/work/"+url.PathEscape(card)), "work"); state != "active" {
		t.Fatalf("source edit was archived instead of fenced: %q", state)
	}
}

type moveCorePair struct {
	source, destination           *liveCoreHarness
	sourceFault, destinationFault *moveIntegrationFaultProxy
}

type moveTopicFixture struct {
	topic, board, document, pinnedDocument, aliasDocument, cardA, cardB, cardBID, unrelatedCard string
}

func newMoveCorePair(t *testing.T) *moveCorePair {
	t.Helper()
	source := newLiveCoreHarness(t)
	destination := newLiveCoreHarness(t)
	sourceCoreURL, destinationCoreURL := source.baseURL, destination.baseURL
	sourceFault, sourceProxy := newMoveIntegrationFaultProxy(t, sourceCoreURL)
	destinationFault, destinationProxy := newMoveIntegrationFaultProxy(t, destinationCoreURL)
	source.baseURL, destination.baseURL = sourceProxy, destinationProxy
	source.enrollHost(t, "move-agent")
	destination.enrollHost(t, "move-agent")
	copyMoveDestinationCredentials(t, source, destination)
	configDir := filepath.Join(source.homeDir, ".config", "anx")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	aliases, _ := json.Marshal(map[string]any{"aliases": map[string]string{"destination": destination.baseURL}})
	if err := os.WriteFile(filepath.Join(configDir, "workspaces.json"), aliases, 0o600); err != nil {
		t.Fatal(err)
	}
	return &moveCorePair{source: source, destination: destination, sourceFault: sourceFault, destinationFault: destinationFault}
}

func (p *moveCorePair) createTopic(t *testing.T, name string) string {
	t.Helper()
	topic := p.source.runCLIExpectOK(t, "move-agent", map[string]any{"topic": map[string]any{
		"title": "Move topic " + name, "summary": "Synthetic topic move fixture",
		"owner_refs": []string{}, "document_refs": []string{}, "board_refs": []string{}, "related_refs": []string{},
		"provenance": map[string]any{"sources": []string{"inferred"}},
	}}, "topics", "create")
	return mustStringPath(t, topic.Payload, "result.topic.ref")
}

func (p *moveCorePair) createCard(t *testing.T, title string, source map[string]any, board string) string {
	return p.createCardWithFields(t, title, source, board, nil)
}

func (p *moveCorePair) createCardWithFields(t *testing.T, title string, source map[string]any, board string, fields map[string]any) string {
	t.Helper()
	input := map[string]any{"title": "Move " + title, "summary": "Synthetic move card", "source": source, "related_refs": []string{}}
	for key, value := range fields {
		input[key] = value
	}
	if board != "" {
		input["board_ref"] = board
	}
	created := p.source.runCLIExpectOK(t, "move-agent", input, "work", "create", "--from-file", "-")
	return mustStringPath(t, created.Payload, "result.work.ref")
}

func (p *moveCorePair) createTopicFixture(t *testing.T, suffix string) moveTopicFixture {
	t.Helper()
	topic := p.createTopic(t, suffix)
	boardResult := p.source.runCLIExpectOK(t, "move-agent", map[string]any{"board": map[string]any{
		"title": "Shared Tasks " + suffix, "refs": []string{"topic:" + strings.TrimPrefix(topic, "topic:")},
		"document_refs": []string{}, "pinned_refs": []string{}, "provenance": map[string]any{"sources": []string{"inferred"}},
	}}, "boards", "create")
	board := mustStringPath(t, boardResult.Payload, "result.board.ref")
	docResult := p.source.runCLIExpectOK(t, "move-agent", map[string]any{
		"document": map[string]any{"id": "move-doc-" + suffix, "title": "Move doc " + suffix},
		"refs":     []string{"topic:" + strings.TrimPrefix(topic, "topic:")},
		"content":  "Synthetic topic move document.", "content_type": "text",
		"provenance": map[string]any{"sources": []string{"inferred"}},
	}, "docs", "create")
	document := mustStringPath(t, docResult.Payload, "result.document.ref")
	pinnedResult := p.source.runCLIExpectOK(t, "move-agent", map[string]any{
		"document": map[string]any{"id": "move-pinned-doc-" + suffix, "title": "Pinned move doc " + suffix},
		"refs":     []string{},
		"content":  "Synthetic card-pinned document.", "content_type": "text",
		"provenance": map[string]any{"sources": []string{"inferred"}},
	}, "docs", "create")
	pinnedDocument := mustStringPath(t, pinnedResult.Payload, "result.document.ref")
	cardB := p.createCardWithFields(t, "owned-b-"+suffix, map[string]any{"authority": "nexus"}, board, map[string]any{"document_ref": pinnedDocument})
	cardBIdentity := integrationMap(p.source.getCore(t, "/cards/"+url.PathEscape(cardB))["card"])
	cardBUUIDRef := "card:" + moveIntegrationString(cardBIdentity["id"])
	aliasResult := p.source.runCLIExpectOK(t, "move-agent", map[string]any{
		"document": map[string]any{"id": "move-card-link-doc-" + suffix, "title": "Card link doc " + suffix},
		"refs":     []string{cardB, cardBUUIDRef},
		"content":  "Synthetic card-linked document.", "content_type": "text",
		"provenance": map[string]any{"sources": []string{"inferred"}},
	}, "docs", "create")
	aliasDocument := mustStringPath(t, aliasResult.Payload, "result.document.ref")
	cardA := p.createCardWithFields(t, "owned-a-"+suffix, map[string]any{"authority": "nexus"}, board, map[string]any{
		"related_refs": []string{cardB, cardBUUIDRef, aliasDocument},
	})
	unrelated := p.createCard(t, "unrelated-"+suffix, map[string]any{"authority": "nexus"}, board)
	patch := map[string]any{"patch": map[string]any{
		"document_refs": []string{document}, "board_refs": []string{board}, "related_refs": []string{cardA, cardB},
	}}
	p.source.runCLIExpectOK(t, "move-agent", patch, "topics", "patch", "--topic-id", topic)
	plan := map[string]any{"steps": []any{
		map[string]any{"id": "verify-b", "title": "Verify linked card", "ref": cardB},
		map[string]any{"id": "verify-b-uuid", "title": "Verify linked card by UUID", "ref": cardBUUIDRef},
	}}
	p.source.runCLIExpectOK(t, "move-agent", plan, "plan", "set", cardA, "--from-file", "-")
	return moveTopicFixture{topic: topic, board: board, document: document, pinnedDocument: pinnedDocument, aliasDocument: aliasDocument, cardA: cardA, cardB: cardB, cardBID: moveIntegrationString(cardBIdentity["id"]), unrelatedCard: unrelated}
}

func newMoveIntegrationFaultProxy(t *testing.T, target string) (*moveIntegrationFaultProxy, string) {
	t.Helper()
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	fault := &moveIntegrationFaultProxy{target: parsed}
	server := httptest.NewServer(http.HandlerFunc(fault.ServeHTTP))
	fault.server = server
	t.Cleanup(server.Close)
	return fault, server.URL
}

type moveIntegrationFaultRule struct {
	method, path, phase string
	source, after       bool
	isRefsWrite         bool
	afterHook           func()
}

type moveIntegrationFaultProxy struct {
	target *url.URL
	server *httptest.Server
	mu     sync.Mutex
	rule   moveIntegrationFaultRule
	armed  bool
	fired  bool
}

func (p *moveIntegrationFaultProxy) arm(rule moveIntegrationFaultRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rule, p.armed, p.fired = rule, true, false
}

func (p *moveIntegrationFaultProxy) wasFired() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fired
}

func (p *moveIntegrationFaultProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	if r.Body != nil {
		raw, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	p.mu.Lock()
	rule := p.rule
	match := p.armed && moveIntegrationFaultMatches(rule, r.Method, r.URL.Path, raw)
	if match && !rule.after {
		p.armed, p.fired = false, true
	}
	p.mu.Unlock()
	if match && !rule.after {
		http.Error(w, "injected move interruption before core write", http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(p.target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
	if match && rule.after {
		proxy.ModifyResponse = func(response *http.Response) error {
			if response.StatusCode < 400 {
				p.mu.Lock()
				p.armed, p.fired = false, true
				p.mu.Unlock()
				if rule.afterHook != nil {
					rule.afterHook()
				}
				return errors.New("injected move response loss after the core write")
			}
			return nil
		}
	}
	proxy.ServeHTTP(w, r)
}

func moveIntegrationFaultMatches(rule moveIntegrationFaultRule, method, path string, body []byte) bool {
	if rule.method != method || !strings.HasPrefix(path, rule.path) {
		return false
	}
	if rule.phase == "snapshot" || rule.phase == "create_destination" {
		return method == http.MethodPost && path == "/topics"
	}
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return false
	}
	patch := integrationMap(request["patch"])
	if rule.isRefsWrite {
		return method == http.MethodPatch && patch != nil && (patch["document_refs"] != nil || patch["related_refs"] != nil)
	}
	marker := integrationMap(patch["workspace_move"])
	return moveIntegrationString(marker["phase"]) == rule.phase
}

func moveIntegrationPhaseRule(phase string) moveIntegrationFaultRule {
	rule := moveIntegrationFaultRule{method: http.MethodPatch, path: "/topics/", phase: phase, after: true}
	switch phase {
	case "snapshot":
		rule.method, rule.path, rule.after = http.MethodPost, "/topics", false
	case "create_destination":
		rule.method, rule.path, rule.after = http.MethodPost, "/topics", true
	case "refs_rewritten":
		rule.phase, rule.isRefsWrite = "refs_rewritten", true
	case "source_transition", "complete":
		rule.source = true
	}
	return rule
}

func copyMoveDestinationCredentials(t *testing.T, source, destination *liveCoreHarness) {
	t.Helper()
	sourceRoot := filepath.Join(source.homeDir, ".config", "anx", "hosts")
	destinationRoot := filepath.Join(destination.homeDir, ".config", "anx", "hosts")
	entries, err := os.ReadDir(destinationRoot)
	if err != nil {
		t.Fatalf("read destination host credentials: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		from := filepath.Join(destinationRoot, entry.Name())
		to := filepath.Join(sourceRoot, entry.Name())
		if err := os.MkdirAll(to, 0o700); err != nil {
			t.Fatal(err)
		}
		hostBytes, err := os.ReadFile(filepath.Join(from, "host.json"))
		if err != nil {
			t.Fatal(err)
		}
		var host map[string]any
		if err := json.Unmarshal(hostBytes, &host); err != nil {
			t.Fatal(err)
		}
		keySource := moveIntegrationString(host["private_key_path"])
		keyBytes, err := os.ReadFile(keySource)
		if err != nil {
			t.Fatal(err)
		}
		keyTarget := filepath.Join(to, "host.ed25519")
		if err := os.WriteFile(keyTarget, keyBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		host["private_key_path"] = keyTarget
		copy, _ := json.MarshalIndent(host, "", "  ")
		if err := os.WriteFile(filepath.Join(to, "host.json"), append(copy, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func removeMoveDestinationCredential(t *testing.T, pair *moveCorePair) {
	t.Helper()
	root := filepath.Join(pair.source.homeDir, ".config", "anx", "hosts")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "host.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var host map[string]any
		if json.Unmarshal(data, &host) == nil && strings.TrimRight(moveIntegrationString(host["base_url"]), "/") == strings.TrimRight(pair.destination.baseURL, "/") {
			if err := os.RemoveAll(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("copied destination-local host credential not found")
}

func (h *liveCoreHarness) getCore(t *testing.T, path string) map[string]any {
	t.Helper()
	body, status := h.getCoreStatus(t, path)
	if status >= 400 {
		t.Fatalf("GET %s returned %d", path, status)
	}
	return body
}

func (h *liveCoreHarness) getCoreStatus(t *testing.T, path string) (map[string]any, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.adminToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out, resp.StatusCode
}

func moveIntegrationState(response map[string]any, key string) string {
	object := integrationMap(response[key])
	if state := moveIntegrationString(object["state"]); state != "" {
		return state
	}
	if moveIntegrationString(object["archived_at"]) != "" {
		return "archived"
	}
	if moveIntegrationString(object["trashed_at"]) != "" {
		return "trashed"
	}
	if moveIntegrationString(object["id"]) != "" || moveIntegrationString(object["ref"]) != "" {
		return "active"
	}
	return ""
}

func integrationContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func countIntegrationRef(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

func moveIntegrationDeterministicUUID(moveID, kind, sourceRef string) string {
	sum := sha1.Sum([]byte(moveID + "\n" + kind + "\n" + sourceRef))
	value := append([]byte(nil), sum[:16]...)
	value[6] = value[6]&0x0f | 0x50
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func integrationMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func integrationSlice(value any) []any {
	result, _ := value.([]any)
	return result
}

func integrationStrings(value any) []string {
	values := []string{}
	switch rows := value.(type) {
	case []string:
		return rows
	case []any:
		for _, row := range rows {
			if ref, ok := row.(string); ok {
				values = append(values, ref)
			}
		}
	}
	return values
}

func moveIntegrationString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
