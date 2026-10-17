package bridge

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type Runner func(context.Context, Workspace, Agent, string) error
type Reporter func(context.Context, Registry)
type runningJob struct {
	cancel context.CancelFunc
	done   chan struct{}
	config string
}

// Each namespace/job has one runner. Independent queues and child-execution
// budgets keep long/offline callbacks out of the heartbeat and short-work lanes.
func Supervise(parent context.Context, root string, run Runner, report Reporter) error {
	ctx, cancel := context.WithCancel(parent)
	children := map[string]runningJob{}
	var wg sync.WaitGroup
	defer func() {
		cancel()
		for _, job := range children {
			job.cancel()
		}
		wg.Wait()
	}()
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	heartbeat := time.NewTicker(60 * time.Second)
	defer heartbeat.Stop()
	reports := make(chan Registry, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case r := <-reports:
				report(ctx, r)
			}
		}
	}()
	reportedRevision := int64(-1)
	for {
		r, e := Load(root)
		if e != nil {
			return e
		}
		desired := map[string]bool{}
		for _, w := range r.Workspaces {
			for _, a := range w.Agents {
				for kind, enabled := range a.Jobs {
					if !enabled || kind == "wake" && a.Jobs["answers"] {
						continue
					}
					k := AgentKey(w, a) + "\x00" + kind
					desired[k] = true
					raw, _ := json.Marshal(struct {
						W Workspace
						A Agent
					}{w, a})
					fingerprint := string(raw)
					if old, ok := children[k]; ok {
						if old.config == fingerprint {
							continue
						}
						old.cancel()
						select {
						case <-old.done:
							delete(children, k)
						default:
							continue
						}
					}
					child, stop := context.WithCancel(ctx)
					done := make(chan struct{})
					children[k] = runningJob{stop, done, fingerprint}
					wg.Add(1)
					go func(w Workspace, a Agent, kind string) {
						defer wg.Done()
						defer close(done)
						for child.Err() == nil {
							_ = run(child, w, a, kind)
							timer := time.NewTimer(3 * time.Second)
							select {
							case <-child.Done():
								timer.Stop()
							case <-timer.C:
							}
						}
					}(w, a, kind)
				}
			}
		}
		for k, job := range children {
			if !desired[k] {
				job.cancel()
				select {
				case <-job.done:
					delete(children, k)
				default:
				}
			}
		}
		if reportedRevision != r.Revision {
			select {
			case reports <- r:
				reportedRevision = r.Revision
			default:
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		case <-heartbeat.C:
			select {
			case reports <- r:
			default:
			}
		}
	}
}
