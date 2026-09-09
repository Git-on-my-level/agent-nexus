package observation

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRuntimePersistsRealReadUnderDurableClaim(t *testing.T) {
	target := fixtureTarget()
	target.Source = "github"
	target.Repository = "o/r"
	var mu sync.Mutex
	state := "idle"
	submitted := 0
	finished := 0
	var submittedBody map[string]any
	callbacks := RuntimeCallbacks{
		GetWork: func(context.Context, string) (map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"source": map[string]any{"authority": "github", "connection_id": "c", "native_id": "o/r#1"}, "refresh": map[string]any{"state": state}}, nil
		},
		Request: func(context.Context, string, string) (map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			state = "queued"
			return map[string]any{}, nil
		},
		Claim: func(context.Context, string, string, time.Duration) (map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			state = "running"
			return map[string]any{"lease_token": "fixture-lease"}, nil
		},
		Submit: func(_ context.Context, actor, card string, body map[string]any) (map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			submitted++
			submittedBody = body
			return map[string]any{}, nil
		},
		Finish: func(_ context.Context, card, lease string, body map[string]any) (map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			finished++
			state = body["state"].(string)
			if lease != "fixture-lease" {
				t.Error("wrong lease")
			}
			return body, nil
		},
	}
	reader := fixtureReader{read: func(ctx context.Context, t Target) (Report, error) { return finishReport(newReport(t, "fixture")) }}
	runtime, err := NewRuntime(callbacks, []Registration{{CardRef: "card:fixture", Target: target, Policy: fixturePolicy(), Reader: reader}}, "service-actor", "fixture-worker", 2)
	if err != nil {
		t.Fatal(err)
	}
	results := runtime.Tick(context.Background())
	if len(results) != 1 || results[0].Error != nil || submitted != 1 || finished != 1 || state != "succeeded" || submittedBody["reader_id"] != "fixture" {
		t.Fatalf("read not durably finished: %+v %d %d %s", results, submitted, finished, state)
	}
	callbacks.GetWork = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"source": map[string]any{"authority": "github", "connection_id": "other", "native_id": "o/r#1"}}, nil
	}
	runtime, err = NewRuntime(callbacks, []Registration{{CardRef: "card:fixture", Target: target, Policy: fixturePolicy(), Reader: reader}}, "service-actor", "fixture-worker", 1)
	if err != nil {
		t.Fatal(err)
	}
	results = runtime.Tick(context.Background())
	if results[0].Error == nil || submitted != 1 {
		t.Fatal("runtime read a changed source binding")
	}
}
