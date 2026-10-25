package primitives_test

import (
	"context"
	"fmt"
	"testing"

	"agent-nexus-core/internal/testutil/perfguard"
)

// Keep preparation work visible alongside executed VM steps. A trigger WHEN
// can make an execution cheap while its nested programs remain expensive to
// prepare. Each sample rolls back its writes and uses the production triggers.
func BenchmarkCanonicalAdmissionWrite(b *testing.B) {
	for _, shape := range []string{"public", "private"} {
		b.Run(shape, func(b *testing.B) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer ws.Close()
			for _, q := range []string{
				`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('write-thread','now','owner','{}')`,
				`INSERT INTO cards(id,handle,thread_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('write-card','write-card','write-thread','Public','','ready','now','owner','now','owner')`,
			} {
				if _, err = ws.DB().Exec(q); err != nil {
					b.Fatal(err)
				}
			}
			if shape == "private" {
				if _, err = ws.DB().Exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='write-thread'`); err != nil {
					b.Fatal(err)
				}
			}
			operations := []struct {
				name, query string
				args        func(int) []any
			}{
				{"attribution", `UPDATE threads SET updated_by=? WHERE id='write-thread'`, func(i int) []any { return []any{fmt.Sprint(i)} }},
				{"title", `UPDATE cards SET title=? WHERE id='write-card'`, func(i int) []any { return []any{fmt.Sprintf("Title %d", i)} }},
				{"event", `INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,'message_posted','now','owner','write-thread','[]','{}')`, func(i int) []any { id := fmt.Sprintf("write-event-%d", i); return []any{id, id} }},
			}
			for _, op := range operations {
				b.Run(op.name, func(b *testing.B) {
					db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
					if err != nil {
						b.Fatal(err)
					}
					defer db.Close()
					tx, err := db.BeginTx(ctx, nil)
					if err != nil {
						b.Fatal(err)
					}
					defer tx.Rollback()
					capture.Start()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if _, err := tx.ExecContext(ctx, op.query, op.args(i)...); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
					_, calls, _ := capture.Stop()
					if err := capture.WorkError(); err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(capture.Work().VMSteps)/float64(b.N), "VM/write")
					b.ReportMetric(float64(calls)/float64(b.N), "statements/write")
					rows, err := tx.QueryContext(ctx, "EXPLAIN "+op.query, op.args(b.N+1)...)
					if err != nil {
						b.Fatal(err)
					}
					count := 0
					for rows.Next() {
						count++
					}
					if err := rows.Err(); err != nil {
						b.Fatal(err)
					}
					rows.Close()
					b.ReportMetric(float64(count), "compiled-ops")
				})
			}
		})
	}
}
