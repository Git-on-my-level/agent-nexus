package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

// Run the actual online worker, including installation, adaptive backfills,
// certification and cutover. The fixture is entirely synthetic.
func TestPerformanceCompactAccessOnlineMigration(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("performance CI tier")
	}
	w := compactFixture(t, 32768)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	start := time.Now()
	reports := 0
	var proof CompactAccessVerification
	var failure error
	w.RunCompactAccessMigration(ctx, func(result CompactAccessVerification, err error) {
		reports++
		proof, failure = result, err
		t.Logf("online migration report rows=%d elapsed=%s error=%v", result.Rows, time.Since(start), err)
		if err == nil {
			cancel()
		}
	})
	if failure != nil || reports != 1 || proof.Rows != 32768 {
		t.Fatalf("worker did not certify: reports=%d rows=%d err=%v context=%v", reports, proof.Rows, failure, ctx.Err())
	}
	var reader int
	if err := w.db.QueryRow(`SELECT reader FROM resource_access_compact_control WHERE singleton=1`).Scan(&reader); err != nil || reader != 1 {
		t.Fatalf("compact reader=%d error=%v", reader, err)
	}
}
