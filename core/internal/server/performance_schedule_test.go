package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type performanceOwnerStage struct {
	Source   string `json:"source"`
	Worker   int    `json:"worker"`
	Success  bool   `json:"success"`
	Finished int64  `json:"finished_unix_ns"`
}

type performanceStageSchedule struct {
	Policy     string                        `json:"policy"`
	WaitMS     float64                       `json:"wait_ms"`
	Owners     map[int]performanceOwnerStage `json:"owners"`
	ReleasedNS int64                         `json:"released_unix_ns"`
}

func publishPerformanceOwnerStage(dir, source string, worker int, success bool) error {
	stage := performanceOwnerStage{source, worker, success, time.Now().UnixNano()}
	body, err := json.Marshal(stage)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("owner-%d.json", worker))
	if err = os.WriteFile(path+".new", body, 0600); err != nil {
		return err
	}
	return os.Rename(path+".new", path)
}

func waitPerformanceOwnerStages(ctx context.Context, dir, source string) (performanceStageSchedule, error) {
	started := time.Now()
	schedule := performanceStageSchedule{Policy: "owners-before-denials", Owners: map[int]performanceOwnerStage{}}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		for worker := 1; worker <= 2; worker++ {
			if _, ok := schedule.Owners[worker]; ok {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("owner-%d.json", worker)))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return schedule, err
			}
			var stage performanceOwnerStage
			if err = json.Unmarshal(body, &stage); err != nil {
				return schedule, err
			}
			if stage.Source != source || stage.Worker != worker || stage.Finished <= 0 {
				return schedule, fmt.Errorf("stale or invalid owner stage %d", worker)
			}
			schedule.Owners[worker] = stage
		}
		if len(schedule.Owners) == 2 {
			schedule.WaitMS = float64(time.Since(started)) / float64(time.Millisecond)
			schedule.ReleasedNS = time.Now().UnixNano()
			return schedule, nil // Failed predecessors release peers; callers preserve failure.
		}
		select {
		case <-ctx.Done():
			return schedule, ctx.Err()
		case <-tick.C:
		}
	}
}

func TestPerformanceOwnerStageBarrier(t *testing.T) {
	dir := t.TempDir()
	const source = "current-source"
	if err := publishPerformanceOwnerStage(dir, source, 1, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitPerformanceOwnerStages(ctx, dir, source); err != context.Canceled {
		t.Fatalf("missing peer did not honor deadline: %v", err)
	}
	if err := publishPerformanceOwnerStage(dir, source, 2, false); err != nil {
		t.Fatal(err)
	}
	schedule, err := waitPerformanceOwnerStages(context.Background(), dir, source)
	if err != nil || schedule.Owners[2].Success || !schedule.Owners[1].Success {
		t.Fatalf("failed peer did not release with its failure preserved: %+v %v", schedule, err)
	}
	for _, stage := range schedule.Owners {
		if schedule.ReleasedNS < stage.Finished {
			t.Fatal("release predates owner completion")
		}
	}
	if _, err = waitPerformanceOwnerStages(context.Background(), dir, "stale-source"); err == nil {
		t.Fatal("stale source marker accepted")
	}
}
