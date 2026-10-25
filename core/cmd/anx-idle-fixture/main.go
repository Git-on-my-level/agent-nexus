// anx-idle-fixture creates or settles marked synthetic data only.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
	"path/filepath"
)

func main() {
	root := flag.String("root", "", "new workspace directory (required)")
	events := flag.Int("events", 8192, "historical messages")
	settleOnly := flag.Bool("settle", false, "drain real maintenance on a marked synthetic fixture before idle measurement")
	flag.Parse()
	if *root == "" {
		log.Fatal("--root required")
	}
	if _, err := os.Stat(storage.NewLayout(*root).DatabasePath); !os.IsNotExist(err) && !*settleOnly {
		log.Fatal("refusing existing database")
	}
	if *settleOnly {
		marker, err := os.ReadFile(*root + "/.synthetic-idle-fixture")
		if err != nil || string(marker) != "synthetic idle fixture\n" {
			log.Fatal("missing synthetic marker")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	w, err := storage.InitializeWorkspace(ctx, *root)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()
	if *settleOnly {
		if err = settle(ctx, w); err != nil {
			log.Fatal(err)
		}
		return
	}
	if !*settleOnly {
		if err = os.WriteFile(*root+"/.synthetic-idle-fixture", []byte("synthetic idle fixture\n"), 0600); err != nil {
			log.Fatal(err)
		}
		key := base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(make([]byte, 32)))
		if err = os.WriteFile(filepath.Join(*root, ".synthetic-heartbeat-key"), []byte(key), 0600); err != nil {
			log.Fatal(err)
		}
		if err = perfguard.Seed(ctx, w.DB(), w.Layout().ArtifactContentDir, "idle-owner", time.Now()); err != nil {
			log.Fatal(err)
		}
	}
	// Historical messages are real canonical rows with active triggers. Large
	// bodies represent long-lived discussion; they are not filler blob files.
	body := strings.Repeat("Synthetic historical discussion with decisions and supporting evidence. ", 256)
	for start := 0; !*settleOnly && start < *events; start += 256 {
		end := min(start+256, *events)
		_, err = w.DB().ExecContext(ctx, `INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json)
   WITH RECURSIVE s(n) AS (SELECT ? UNION ALL SELECT n+1 FROM s WHERE n+1<?)
   SELECT 'idle-history-'||printf('%08d',n),'idle-history-'||n,'message_posted','2026-01-01T00:00:00Z','idle-owner','scale-thread-'||(n%4096),'[]',json_object('text',?) FROM s`, start, end, body)
		if err != nil {
			log.Fatal(err)
		}
		if start%4096 == 0 {
			log.Printf("events=%d", end)
		}
	}
	for r := 2; r <= 8; r++ {
		_, err = w.DB().ExecContext(ctx, `INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,thread_id,created_at,created_by)
   SELECT 'idle-rev-'||?||'-'||id,id,?,'scale-artifact-'||substr(id,11),thread_id,'2026-01-01T00:00:00Z','idle-owner' FROM documents WHERE id LIKE 'scale-doc-%'`, r, r)
		if err != nil {
			log.Fatal(err)
		}
	}
	_, err = w.DB().ExecContext(ctx, `INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json)
  SELECT 'idle-ask-'||id,'idle-ask-'||id,'human_attention_requested','2026-01-01T00:00:00Z','idle-owner',thread_id,'[]',json_object('payload',json_object('title','Synthetic open ask','requester_actor_id','idle-owner','subject_ref','card:'||id)) FROM cards WHERE id LIKE 'scale-card-%'`)
	if err != nil {
		log.Fatal(err)
	}
	_, err = w.DB().ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json,created_at)
 WITH RECURSIVE s(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM s WHERE n<32767)
 SELECT 'synthetic','idle-owner','receipt-'||n,'synthetic',200,json_object('synthetic_response',?),CURRENT_TIMESTAMP FROM s`, strings.Repeat("x", 16384))
	if err != nil {
		log.Fatal(err)
	}
	if _, err = w.DB().ExecContext(ctx, "ANALYZE; VACUUM; PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Fatal(err)
	}
	info, err := os.Stat(w.Layout().DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("database_bytes=%d events=%d documents=4096 revisions=32768 asks=4096 receipts=32768 cards=4096 plans=4096\n", info.Size(), *events+8192)
}

func settle(ctx context.Context, w *storage.Workspace) error {
	// Retained replay responses are real JSON content, not allocated filler.
	if _, err := w.DB().ExecContext(ctx, `UPDATE idempotency_replays SET response_json=json_object('synthetic_response',?) WHERE scope='synthetic' AND length(response_json)<16384`, strings.Repeat("x", 16384)); err != nil {
		return err
	}
	s := primitives.NewStore(w.DB(), blob.NewFilesystemBackend(w.Layout().ArtifactContentDir), w.Layout().ArtifactContentDir)
	drain := func(name string, step func(context.Context) (bool, error)) error {
		log.Printf("settle_started=%s", name)
		for n := 0; ; n++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			done, err := step(ctx)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if done {
				log.Printf("settle_finished=%s passes=%d", name, n+1)
				return nil
			}
		}
	}
	if err := drain("ask_subjects", w.MaintainAskSubjectsBatch); err != nil {
		return err
	}
	if err := drain("inbox", func(c context.Context) (bool, error) { return w.MaintainInboxLifecycleBatch(c, 200) }); err != nil {
		return err
	}
	if err := drain("brief", func(c context.Context) (bool, error) { return s.RefreshBriefInitiatives(c, 200) }); err != nil {
		return err
	}
	if err := drain("comments", w.MaintainCommentSearchBatch); err != nil {
		return err
	}
	if err := drain("documents", s.MaintainDocumentSearchBatch); err != nil {
		return err
	}
	var mode int
	if err := w.DB().QueryRowContext(ctx, `SELECT reader FROM resource_access_compact_control WHERE singleton=1`).Scan(&mode); err != nil {
		return err
	}
	if mode != 1 {
		if err := w.InitializeCompactAccessShadow(ctx); err != nil {
			return err
		}
		if err := drain("compact", w.MaintainCompactAccessBatch); err != nil {
			return err
		}
		if err := drain("stable", w.MaintainStableAccessBatch); err != nil {
			return err
		}
		proof, err := w.VerifyCompactAccessCutover(ctx)
		if err != nil {
			return err
		}
		if err = w.CutoverCompactAccess(ctx, proof); err != nil {
			return err
		}
		w.RunCompactAccessRetiredMaintenance(ctx)

	}
	var err error
	if err = drain("document_history", w.MaintainDocumentAccessHistoryBatch); err != nil {
		return err
	}
	// Exercise the rollback reader's own repair path, retaining its strict
	// refusal to serve stale aggregates until the dirty queue is empty.
	previous := os.Getenv("ANX_INDEPENDENT_DOCUMENT_SEARCH")
	os.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", "false")
	defer os.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", previous)
	log.Printf("settle_started=legacy_search")
	for {
		var pending bool
		if err = w.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM document_search_dirty)`).Scan(&pending); err != nil {
			return err
		}
		if !pending {
			break
		}
		if _, _, err = s.SearchDocuments(ctx, primitives.DocumentSearchFilter{Query: "synthetic"}); err != nil && !errors.Is(err, primitives.ErrSearchRepairPending) {
			return err
		}
	}
	log.Printf("settle_finished=legacy_search")
	clock := delivery.Escalator{Store: delivery.Store{DB: w.DB()}, CanRead: func(context.Context, string, string) bool { return false }}
	for range 24 {
		if err = clock.Step(ctx, time.Now().UTC()); err != nil {
			return err
		}
	}
	state, err := router.NewStateStore(filepath.Join(w.Layout().RootDir, "router", "router-state.json"))
	if err != nil {
		return err
	}
	var ts, id string
	if err = w.DB().QueryRowContext(ctx, `SELECT ts,id FROM events WHERE type='message_posted' ORDER BY julianday(ts) DESC,id DESC LIMIT 1`).Scan(&ts, &id); err != nil {
		return err
	}
	if err = state.SetLastEventCursor(ts, id); err != nil {
		return err
	}
	_, err = w.DB().ExecContext(ctx, `VACUUM; PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}
