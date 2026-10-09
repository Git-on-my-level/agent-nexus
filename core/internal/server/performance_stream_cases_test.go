package server

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/testutil/perfguard"
)

const performanceStreamThread = "scale-stream-large-thread"
const performanceInboxThread = "scale-stream-inbox-thread"

// Setup uses the ordinary SQLite connection, with canonical reference triggers
// enabled. Only the handler's reads enter the measured capture.
func preparePerformanceStreamCases(t *testing.T, env performanceEnv, budgets []routeBudget) performanceEnv {
	t.Helper()
	ctx := context.Background()
	tx, err := env.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, thread := range []string{performanceStreamThread, performanceInboxThread} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,?,?,'{}')`, thread, perfguard.Timestamp, env.principals[0].ActorID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `WITH RECURSIVE history(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM history WHERE n+1 < ?)
INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json)
SELECT printf('scale-stream-history-%04d',n),printf('scale-stream-history-%04d',n),'message_posted',?,?,?,'[]',json_object('text',printf('Public stream history %04d',n)) FROM history`, perfguard.Rows, perfguard.Timestamp, env.principals[0].ActorID, performanceStreamThread); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('scale-stream-history-0200-private','scale-stream-history-0200-private','message_posted',?,?,?,json_array(?),json_object('text','PrivateStreamHistoryControl'))`, perfguard.Timestamp, env.principals[0].ActorID, performanceStreamThread, env.replace.Replace("{card_ref}")); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Preserve all selectors used by the existing budgets while adding the
	// scenario selectors. Derive their names from the inventory, not a second
	// manually maintained list of ordinary-route fixture fields.
	raw, err := json.Marshal(budgets)
	if err != nil {
		t.Fatal(err)
	}
	pairs := []string{"{stream_thread_id}", performanceStreamThread, "{stream_resume_id}", "scale-stream-history-4094", "{stream_idle_id}", "scale-stream-idle-cursor", "{inbox_resume_id}", "scale-inbox-resume-cursor", "{inbox_idle_id}", "scale-inbox-idle-cursor"}
	seen := map[string]bool{}
	for _, token := range regexp.MustCompile(`\{[a-z_]+\}`).FindAllString(string(raw)+"{card_ref}{thread_id}", -1) {
		if !seen[token] && env.replace.Replace(token) != token {
			pairs = append(pairs, token, env.replace.Replace(token))
			seen[token] = true
		}
	}
	env.replace = strings.NewReplacer(pairs...)
	return env
}

// The first completed poll installs fresh controls. The second completed poll
// must deliver them. Counting Flush alone cannot establish either freshness or
// visibility; validation also examines the two distinct response segments.
func configurePerformanceStreamCase(t *testing.T, env performanceEnv, b routeBudget, principal, sample int, req *http.Request, w *performanceWriter) func() {
	t.Helper()
	if b.Scenario == "" {
		return func() {}
	}
	if principal == 1 && b.UnauthorizedStatus == http.StatusNotFound {
		return func() {} // Denied private-thread streams must return before any payload.
	}
	firstEnd := -1
	public := fmt.Sprintf("FreshStreamPublic-%s-%d-%d", b.Scenario, principal, sample)
	private := fmt.Sprintf("FreshStreamPrivate-%s-%d-%d", b.Scenario, principal, sample)
	checkPrivate := false
	freshID := ""
	cleanup := func() {}
	var firstMust, firstMustNot string
	switch b.Scenario {
	case "receipts-private-thread", "receipts-public-thread":
		req.Header.Del("Last-Event-ID") // Snapshot controls require the default history path.
		public = time.Date(2030, 1, 1, 0, principal, sample, 0, time.UTC).Format(time.RFC3339)
		private = time.Date(2031, 1, 1, 0, principal, sample, 0, time.UTC).Format(time.RFC3339)
		thread := req.URL.Query().Get("thread_id")
		id := fmt.Sprintf("scale-receipt-control-%s-%d", b.Scenario, principal)
		firstMust = id + "-initial"
		checkPrivate = b.Scenario == "receipts-public-thread"
		removeReceipts := func() {
			if _, err := env.db.Exec(`DELETE FROM agent_wakeups WHERE wakeup_id IN (?,?,?,?)`, id+"-initial", id+"-initial-private", id+"-public", id+"-private"); err != nil {
				t.Errorf("remove temporary receipt controls: %v", err)
			}
		}
		if sample == 0 {
			t.Cleanup(removeReceipts)
		}
		appendReceipt := func(suffix, text string, refs []string) {
			at := time.Now().UTC().Format(time.RFC3339Nano)
			seedReceiptStreamWakeup(t, env.store, primitives.AgentWakeup{WakeupID: id + suffix, ThreadID: thread, TargetHandle: env.principals[0].Username, TargetActorID: env.principals[0].ActorID, Status: primitives.AgentWakeupStatusRequested, TriggerText: text, Refs: refs, CreatedAt: at, UpdatedAt: at})
		}
		if sample == 0 {
			appendReceipt("-initial", "Initial visible receipt", nil)
			if checkPrivate {
				appendReceipt("-initial-private", "InitialStreamReceiptPrivate", []string{env.replace.Replace("{card_ref}")})
			}
		}
		w.onFlush = func() {
			firstEnd = w.Body.Len()
			var before, after int64
			if err := env.db.QueryRow(`SELECT version FROM receipt_access_epoch WHERE singleton=1`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			// Delivery-only writes append replay positions without changing receipt
			// ancestry. Keep authority and cardinality stable across warm requests.
			for suffix, marker := range map[string]string{"-initial": public, "-initial-private": private} {
				if suffix == "-initial-private" && !checkPrivate {
					continue
				}
				if _, err := env.db.Exec(`UPDATE agent_wakeups SET read_at=? WHERE wakeup_id=?`, marker, id+suffix); err != nil {
					t.Fatal(err)
				}
			}
			if err := env.db.QueryRow(`SELECT version FROM receipt_access_epoch WHERE singleton=1`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("delivery-only receipt controls invalidated the warm authority cache")
			}
		}
	case "events-thread", "events-default", "events-large-thread", "events-resumed", "events-idle":
		thread := req.URL.Query().Get("thread_id")
		if thread == "" {
			thread = performanceStreamThread
		}
		checkPrivate = b.Scenario != "events-thread"
		if b.Scenario == "events-idle" {
			// Select a visible cursor after all prior samples. A private cursor
			// would be unknown to the stranger and accidentally replay history.
			var cursor string
			if err := env.db.QueryRow(`SELECT id FROM events WHERE thread_id=? AND refs_json='[]' ORDER BY ts DESC,id DESC LIMIT 1`, thread).Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Last-Event-ID", cursor)
			// On the initial fixture, the private history control sorts after
			// the last public history row. Install a public tail for both roles.
			if cursor == "scale-stream-history-4095" {
				cursor = "scale-stream-initial-public-tail"
				performanceAppendStreamControls(t, env, thread, cursor, "Initial public stream tail", "", "2026-01-02T00:00:00Z")
				req.Header.Set("Last-Event-ID", cursor+"-public")
			}
		} else if b.Scenario == "events-resumed" {
			firstMust, firstMustNot = "scale-stream-history-4095", "scale-stream-history-4094"
		} else if b.Scenario == "events-large-thread" {
			firstMust = "scale-stream-history-0001"
			req.Header.Set("Last-Event-ID", "scale-stream-history-0000")
		}
		id := fmt.Sprintf("scale-stream-poll-%s-%d-%d", b.Scenario, principal, sample)
		freshID = id
		cleaned := false
		cleanup = func() {
			if cleaned {
				return
			}
			// Delete only this request's temporary controls, through the same
			// uninstrumented fixture connection with reference triggers enabled.
			// This runs after Stop, so each measured sample starts with the same
			// canonical event and private-reference graph cardinality.
			if _, err := env.db.Exec(`DELETE FROM events WHERE id IN (?,?)`, id+"-private", id+"-public"); err != nil {
				t.Errorf("remove temporary stream controls: %v", err)
				return
			}
			cleaned = true
		}
		// A request deadline/status failure may bypass the returned validator.
		// Register before insertion so even a failed partial setup is covered.
		t.Cleanup(cleanup)
		w.onFlush = func() {
			firstEnd = w.Body.Len()
			if b.Scenario == "events-large-thread" {
				return
			}
			privateBody := ""
			if checkPrivate {
				privateBody = private
			}
			performanceAppendStreamControls(t, env, thread, id, public, privateBody, time.Now().UTC().Format(time.RFC3339Nano))
		}
	case "inbox-snapshot", "inbox-resumed", "inbox-idle":
		checkPrivate = true
		performanceSeedInboxControls(t, env, "InitialStreamInboxPublic", "InitialStreamInboxPrivate")
		if b.Scenario != "inbox-snapshot" {
			// Obtain the actual principal-filtered first page. This is setup,
			// outside Start, and avoids guessing the contract payload digest.
			listReq := httptest.NewRequest(http.MethodGet, "/inbox?limit=100", nil)
			listReq.Header.Set("Authorization", req.Header.Get("Authorization"))
			list := httptest.NewRecorder()
			setupHandler := env.handler
			if env.setupHandler != nil {
				setupHandler = env.setupHandler
			}
			setupHandler.ServeHTTP(list, listReq)
			var payload struct {
				Items []map[string]any `json:"items"`
			}
			if list.Code != http.StatusOK || json.Unmarshal(list.Body.Bytes(), &payload) != nil {
				t.Fatalf("inbox cursor setup failed: status=%d", list.Code)
			}
			records := buildInboxStreamRecords(payload.Items)
			if len(records) < 2 {
				t.Fatal("inbox cursor scenario needs a populated page")
			}
			index := len(records) - 1
			if b.Scenario == "inbox-resumed" {
				index--
				firstMust = records[index+1].eventID
				firstMustNot = records[index].eventID
			}
			req.Header.Set("Last-Event-ID", records[index].eventID)
		}
		w.onFlush = func() {
			firstEnd = w.Body.Len()
			performanceSeedInboxControls(t, env, public, private)
		}
	case "agents-change", "agents-resumed-header":
		w.onFlush = func() {
			firstEnd = w.Body.Len()
			env.hub.publish()
		}
	default:
		t.Fatalf("unknown performance stream scenario %q", b.Scenario)
	}
	return func() {
		t.Helper()
		defer cleanup()
		if firstEnd < 0 || w.polls != 2 {
			t.Fatal("stream scenario did not complete two polls/invalidation deliveries")
		}
		first, second := w.Body.String()[:firstEnd], w.Body.String()[firstEnd:]
		if strings.HasPrefix(b.Scenario, "receipts-") {
			if err := performanceValidateReceiptFrames(w.Body.String()); err != nil {
				t.Error(err)
			}
			if checkPrivate && principal == 0 && !strings.Contains(first, "InitialStreamReceiptPrivate") {
				t.Error("owner omitted initial private receipt")
			}
			if principal != 0 && (strings.Contains(w.Body.String(), "InitialStreamReceiptPrivate") || strings.Contains(w.Body.String(), "-initial-private") || strings.Contains(w.Body.String(), "-private@")) {
				t.Error("stranger received a private receipt identity or payload")
			}
		}
		if strings.HasPrefix(b.Scenario, "events-") {
			if err := performanceValidateEventFrames(w.Body.String()); err != nil {
				t.Error(err)
			}
		}
		if strings.Contains(w.Body.String(), "event: error") {
			t.Fatalf("stream scenario returned an error: %.500s", w.Body.String())
		}
		if firstMust != "" && !strings.Contains(first, firstMust) {
			t.Errorf("first poll omitted cursor/history control %q", firstMust)
		}
		if firstMustNot != "" && strings.Contains(first, firstMustNot) {
			t.Errorf("resume replayed cursor %q", firstMustNot)
		}
		if strings.HasSuffix(b.Scenario, "idle") && (strings.Contains(first, "event:") || !strings.Contains(first, ": keepalive")) {
			t.Error("idle first poll replayed data or failed to issue a keepalive")
		}
		if strings.HasPrefix(b.Scenario, "agents-") {
			performanceValidateAgentChanges(t, first, second)
			return
		}
		if b.Scenario == "events-large-thread" {
			if err := performanceValidateHistoryStream(first, second, principal == 0); err != nil {
				t.Error(err)
			}
			return
		}
		if b.Scenario == "events-default" {
			if strings.Contains(first, "event:") {
				t.Error("default live stream replayed workspace history")
			}
			if err := performanceValidateLiveStreamIDs(w.Body.String(), freshID, principal == 0); err != nil {
				t.Error(err)
			}
		}
		if strings.Contains(first, public) || !strings.Contains(second, public) {
			t.Error("second poll did not deliver its fresh visible data control")
		}
		if checkPrivate {
			if principal == 0 && !strings.Contains(second, private) {
				t.Error("authorized second poll omitted its fresh private control")
			}
			if principal != 0 && (strings.Contains(w.Body.String(), "FreshStreamPrivate-") || strings.Contains(w.Body.String(), "PrivateStreamHistoryControl") || strings.Contains(w.Body.String(), "InitialStreamInboxPrivate")) {
				t.Error("unrelated principal received a private stream control")
			}
		}
	}
}

// A resume marker may repeat only the last visible data ID, never the hidden
// scanner cursor. Keepalives carry no IDs or payload; resume payload is empty.
func performanceValidateReceiptFrames(body string) error {
	seen := map[string]bool{}
	for _, frame := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(frame) == "" || frame == ": keepalive" {
			continue
		}
		id, kind, data := "", "", ""
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				kind = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			default:
				return fmt.Errorf("malformed receipt frame")
			}
		}
		var payload struct {
			Receipt map[string]any `json:"receipt"`
		}
		if kind != "notification_receipt" || json.Unmarshal([]byte(data), &payload) != nil || payload.Receipt == nil {
			return fmt.Errorf("invalid receipt payload")
		}
		encoded, _ := json.Marshal(payload.Receipt)
		sum := sha1.Sum(encoded)
		want := fmt.Sprintf("receipt:%s@%x", anyString(payload.Receipt["wakeup_id"]), sum[:8])
		if id != want || seen[id] {
			return fmt.Errorf("incorrect or duplicate receipt ID/digest")
		}
		seen[id] = true
	}
	if len(seen) < 2 {
		return fmt.Errorf("receipt stream needs distinct initial and fresh payloads")
	}
	return nil
}

func performanceValidateEventFrames(body string) error {
	lastVisible := ""
	seen := map[string]bool{}
	for _, frame := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(frame) == "" {
			continue
		}
		if frame == ": keepalive" {
			continue
		}
		id, kind, data := "", "", ""
		for _, line := range strings.Split(frame, "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				if id != "" {
					return fmt.Errorf("duplicate stream ID")
				}
				id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				kind = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			default:
				return fmt.Errorf("malformed event stream frame")
			}
		}
		if kind == "resume" {
			var payload map[string]any
			if id == "" || id != lastVisible || json.Unmarshal([]byte(data), &payload) != nil || payload == nil || len(payload) != 0 {
				return fmt.Errorf("resume exposed a hidden/unrelated cursor or payload")
			}
		} else if kind == "event" {
			var payload struct {
				Event map[string]any `json:"event"`
			}
			if id == "" || json.Unmarshal([]byte(data), &payload) != nil || anyString(payload.Event["id"]) != id {
				return fmt.Errorf("invalid visible event frame")
			}
			if seen[id] {
				return fmt.Errorf("stream replayed visible data ID %q", id)
			}
			seen[id] = true
			lastVisible = id
		} else {
			return fmt.Errorf("unexpected event stream kind %q", kind)
		}
	}
	return nil
}

// Resume frames repeat the last visible ID by contract; inspect data events only.
func performanceHistoryIDs(segment string) []string {
	var ids []string
	for _, frame := range strings.Split(segment, "\n\n") {
		if !strings.Contains(frame, "event: event\n") {
			continue
		}
		for _, line := range strings.Split(frame, "\n") {
			if strings.HasPrefix(line, "id: ") {
				ids = append(ids, strings.TrimPrefix(line, "id: "))
			}
		}
	}
	return ids
}
func performanceValidateLiveStreamIDs(body, id string, authorized bool) error {
	want := []string{id + "-public"}
	if authorized {
		want = append([]string{id + "-private"}, want...)
	}
	if fmt.Sprint(performanceHistoryIDs(body)) != fmt.Sprint(want) {
		return fmt.Errorf("default live stream delivered old, duplicate or unexpected event IDs")
	}
	return nil
}

func performanceValidateHistoryStream(first, second string, authorized bool) error {
	wantFirst, wantSecond := []string{}, []string{}
	for n := 1; n <= 200; n++ {
		wantFirst = append(wantFirst, fmt.Sprintf("scale-stream-history-%04d", n))
	}
	if authorized {
		wantSecond = append(wantSecond, "scale-stream-history-0200-private")
	}
	for n := 201; n <= 399; n++ {
		wantSecond = append(wantSecond, fmt.Sprintf("scale-stream-history-%04d", n))
	}
	if fmt.Sprint(performanceHistoryIDs(first)) != fmt.Sprint(wantFirst) || fmt.Sprint(performanceHistoryIDs(second)) != fmt.Sprint(wantSecond) {
		return fmt.Errorf("large history did not deliver exactly two ordered bounded pages (private visible=%v)", authorized)
	}
	return nil
}

func performanceAppendStreamControls(t *testing.T, env performanceEnv, thread, id, public, private, timestamp string) {
	t.Helper()
	tx, err := env.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if private != "" {
		if _, err = tx.Exec(`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,'message_posted',?,?,?,json_array(?),json_object('text',?))`, id+"-private", id+"-private", timestamp, env.principals[0].ActorID, thread, env.replace.Replace("{card_ref}"), private); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,'message_posted',?,?,?,'[]',json_object('text',?))`, id+"-public", id+"-public", timestamp, env.principals[0].ActorID, thread, public); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func performanceSeedInboxControls(t *testing.T, env performanceEnv, public, private string) {
	t.Helper()
	visible := streamPrivacyInboxItem(performanceInboxThread, "scale-stream-inbox-public", public)
	tail := streamPrivacyInboxItem(performanceInboxThread, "scale-stream-inbox-tail", "Stable public inbox cursor control")
	secret := streamPrivacyInboxItem(performanceInboxThread, "scale-stream-inbox-private", private)
	secret.Data["related_refs"] = []any{env.replace.Replace("{card_ref}")}
	seedStreamPrivacyInbox(t, env.store, performanceInboxThread, visible, tail, secret)
}

func performanceValidateAgentChanges(t *testing.T, first, second string) {
	t.Helper()
	parse := func(segment string) uint64 {
		t.Helper()
		if strings.Count(segment, "event: agents_changed\n") != 1 || strings.Contains(segment, "id:") {
			t.Fatal("agent invalidation must contain one cursor-free notification per delivery")
		}
		for _, line := range strings.Split(segment, "\n") {
			if strings.HasPrefix(line, "data: ") {
				var payload struct {
					Revision uint64 `json:"revision"`
				}
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
					t.Fatal(err)
				}
				return payload.Revision
			}
		}
		t.Fatal("agent invalidation omitted data")
		return 0
	}
	if before, after := parse(first), parse(second); after <= before {
		t.Errorf("agent invalidation did not advance: initial=%d next=%d", before, after)
	}
}

func TestPerformanceStreamScenarioInventory(t *testing.T) {
	t.Parallel()
	budgets := performanceBudgets(t)
	want := map[string]string{
		"events-default": "events-default", "events-large-thread": "events-large-thread", "events-resumed": "events-resumed", "events-idle": "events-idle",
		"inbox-resumed": "inbox-resumed", "inbox-idle": "inbox-idle", "agents-resumed-header": "agents-resumed-header",
	}
	for _, b := range budgets {
		if scenario, ok := want[b.Case]; ok {
			if b.Scenario != scenario || b.Method != http.MethodGet || !strings.HasPrefix(b.Path, "/stream/") {
				t.Errorf("incomplete stream scale case %q", b.Case)
			}
			if strings.Contains(b.Scenario, "resumed") || strings.HasSuffix(b.Scenario, "idle") {
				if b.Headers["Last-Event-ID"] == "" {
					t.Errorf("cursor/header case %q needs Last-Event-ID", b.Case)
				}
			}
			delete(want, b.Case)
		}
	}
	for missing := range want {
		t.Errorf("missing stream scale case %q", missing)
	}
}

func TestPerformanceHistoryStreamControls(t *testing.T) {
	t.Parallel()
	frame := func(id string) string { return "id: " + id + "\nevent: event\ndata: {}\n\n" }
	for _, authorized := range []bool{true, false} {
		first, second := "", ""
		for n := 1; n <= 200; n++ {
			first += frame(fmt.Sprintf("scale-stream-history-%04d", n))
		}
		first += "id: scale-stream-history-0200\nevent: resume\ndata: {}\n\n"
		if authorized {
			second += frame("scale-stream-history-0200-private")
		}
		for n := 201; n <= 399; n++ {
			second += frame(fmt.Sprintf("scale-stream-history-%04d", n))
		}
		if err := performanceValidateHistoryStream(first, second, authorized); err != nil {
			t.Fatal(err)
		}
		for _, broken := range []string{second + frame("scale-stream-history-0400"), strings.Replace(second, frame("scale-stream-history-0201"), "", 1), second + frame("scale-stream-history-0201")} {
			if performanceValidateHistoryStream(first, broken, authorized) == nil {
				t.Error("invalid bounded page accepted")
			}
		}
		if !authorized && performanceValidateHistoryStream(first, frame("scale-stream-history-0200-private")+second, false) == nil {
			t.Error("private history accepted")
		}
	}
}

// Exercise the controls against real authentication and canonical privacy on a
// small workspace; the acceptance run supplies the full unrelated corpus.
func TestStreamScaleControlsPrivacy(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	base := newAuthIntegrationEnv(t, authIntegrationOptions{})
	db := base.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, context.Background(), db, "stream-scale-owner", "stream-scale-owner-actor", "stream.scale.owner", "stream-scale-owner-token")
	stranger := seedMachinePrincipalForLockoutTest(t, context.Background(), db, "stream-scale-stranger", "stream-scale-stranger-actor", "stream.scale.stranger", "stream-scale-stranger-token")
	store := base.primitiveStore.(*primitives.Store)
	privateThread := seedStreamPrivacyThread(t, store, owner.ActorID, true)
	var cardRef string
	if err := db.QueryRow(`SELECT 'card:'||id FROM cards WHERE thread_id=?`, privateThread).Scan(&cardRef); err != nil {
		t.Fatal(err)
	}
	for _, thread := range []string{performanceStreamThread, performanceInboxThread} {
		if _, err := db.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,?,?,'{}')`, thread, perfguard.Timestamp, owner.ActorID); err != nil {
			t.Fatal(err)
		}
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var opts *handlerOptions
	handler := NewHandler("stream-scale-controls", WithPrimitiveStore(store), WithAuthStore(base.authStore), WithSchemaContract(contract), WithActorRegistry(base.registry), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})), WithStreamPollInterval(time.Millisecond), func(o *handlerOptions) { opts = o })
	env := performanceEnv{store: store, db: db, handler: handler, principals: []lockoutPrincipalSeed{owner, stranger}, hub: opts.agentChanges, replace: strings.NewReplacer("{card_ref}", cardRef)}
	performanceAppendStreamControls(t, env, performanceStreamThread, "small-history", "Small stream history", "", perfguard.Timestamp)
	for pi, principal := range env.principals {
		for _, scenario := range []string{"events-default", "events-idle", "inbox-snapshot", "inbox-resumed", "inbox-idle", "agents-change", "agents-resumed-header", "receipts-public-thread"} {
			t.Run(fmt.Sprintf("%d/%s", pi, scenario), func(t *testing.T) {
				path := "/stream/events?thread_id=" + performanceStreamThread
				if strings.HasPrefix(scenario, "inbox-") {
					path = "/stream/inbox"
				} else if strings.HasPrefix(scenario, "agents-") {
					path = "/stream/agents"
				} else if strings.HasPrefix(scenario, "receipts-") {
					path = "/stream/agent-notification-receipts?thread_id=" + performanceStreamThread
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
				req.Header.Set("Last-Event-ID", "arbitrary-reconnect-header")
				w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, eventStream: strings.HasPrefix(scenario, "events-"), receiptStream: strings.HasPrefix(scenario, "receipts-")}
				validate := configurePerformanceStreamCase(t, env, routeBudget{Scenario: scenario, UnauthorizedStatus: http.StatusOK}, pi, 0, req, w)
				handler.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("stream status=%d body=%.500s", w.Code, w.Body.String())
				}
				validate()
				var remaining int
				if err := db.QueryRow(`SELECT count(*) FROM events WHERE id IN (?,?)`, fmt.Sprintf("scale-stream-poll-%s-%d-0-private", scenario, pi), fmt.Sprintf("scale-stream-poll-%s-%d-0-public", scenario, pi)).Scan(&remaining); err != nil || remaining != 0 {
					t.Fatalf("validated request retained temporary controls: rows=%d err=%v", remaining, err)
				}
			})
		}
	}
	// Simulate a caller that exits before invoking validation. Its subtest
	// cleanup must remove the fresh controls without touching the initial tail.
	t.Run("caller-skips-validation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/stream/events?thread_id="+performanceStreamThread, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+owner.AccessToken)
		w := &performanceWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel, eventStream: true}
		configurePerformanceStreamCase(t, env, routeBudget{Scenario: "events-idle"}, 0, 99, req, w)
		handler.ServeHTTP(w, req)
	})
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM events WHERE id IN ('scale-stream-poll-events-idle-0-99-private','scale-stream-poll-events-idle-0-99-public')`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("caller cleanup retained temporary controls: rows=%d err=%v", remaining, err)
	}
}

func TestPerformanceResumeMarkersCannotExposeHiddenCursor(t *testing.T) {
	t.Parallel()
	visible := "id: public\nevent: event\ndata: {\"event\":{\"id\":\"public\"}}\n\n"
	valid := visible + "id: public\nevent: resume\ndata: {}\n\n"
	if err := performanceValidateEventFrames(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{valid + visible, strings.Replace(valid, "id: public\nevent: resume", "id: scale-stream-history-0200-private\nevent: resume", 1), visible + "id: public\nevent: resume\ndata: {\"hidden\":\"secret\"}\n\n", "id: public\nevent: resume\ndata: {}\n\n", visible + "event: error\ndata: {}\n\n"} {
		if performanceValidateEventFrames(invalid) == nil {
			t.Error("hidden/invalid resume frame accepted")
		}
	}
}

func TestPerformanceLiveStreamRejectsHistoryReplay(t *testing.T) {
	t.Parallel()
	frame := func(id string) string { return "id: " + id + "\nevent: event\ndata: {}\n\n" }
	for _, authorized := range []bool{true, false} {
		body := frame("fresh-public")
		if authorized {
			body = frame("fresh-private") + body
		}
		if err := performanceValidateLiveStreamIDs(body, "fresh", authorized); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{body + frame("scale-stream-history-0001"), body + frame("fresh-public"), body + frame("unexpected")} {
			if performanceValidateLiveStreamIDs(bad, "fresh", authorized) == nil {
				t.Error("live stream replay/extra event accepted")
			}
		}
	}
}
