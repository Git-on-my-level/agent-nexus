package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
)

const reportWorkers = 4
const reportCacheTTL = 2 * time.Second
const reportCacheCapacity = 64

type reportPanelCache struct {
	mu      sync.Mutex
	entries map[string]cachedReportPanel
}
type cachedReportPanel struct {
	data    []byte
	expires time.Time
}

func (c *reportPanelCache) get(key string, now time.Time) map[string]any {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	if !now.Before(entry.expires) {
		delete(c.entries, key)
		return nil
	}
	var out map[string]any
	if json.Unmarshal(entry.data, &out) != nil {
		return nil
	}
	return out
}
func (c *reportPanelCache) put(key string, value map[string]any, now time.Time) {
	if c == nil || value["status"] == "unavailable" {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 128*1024 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]cachedReportPanel{}
	}
	for k, v := range c.entries {
		if !now.Before(v.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= reportCacheCapacity {
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = cachedReportPanel{raw, now.Add(reportCacheTTL)}
}

func selectReportPanels(r *http.Request, panels []reports.Panel) ([]reports.Panel, error) {
	raw, present := r.URL.Query()["panels"]
	if !present {
		return panels, nil
	}
	if len(raw) != 1 || raw[0] == "" || len(raw[0]) > 2591 {
		return nil, fmt.Errorf("panels must contain 1..32 unique panel IDs")
	}
	wanted := map[string]bool{}
	for _, id := range strings.Split(raw[0], ",") {
		if id == "" || wanted[id] || len(wanted) >= 32 {
			return nil, fmt.Errorf("panels must contain 1..32 unique panel IDs")
		}
		wanted[id] = true
	}
	out := []reports.Panel{}
	for _, p := range panels {
		if wanted[p.ID] {
			out = append(out, p)
			delete(wanted, p.ID)
		}
	}
	if len(wanted) > 0 {
		return nil, fmt.Errorf("unknown panel ID")
	}
	return out, nil
}

func materializeReportPanels(r *http.Request, opts handlerOptions, panels []reports.Panel) (string, []map[string]any) {
	now := time.Now().UTC()
	results := make([]map[string]any, len(panels))
	// Equal live queries share work even if authored under different IDs or roles.
	type job struct {
		panel   reports.Panel
		indices []int
		key     string
	}
	jobs := []job{}
	byKey := map[string]int{}
	for i, p := range panels {
		identity := p
		identity.ID = ""
		identity.Role = ""
		raw, _ := json.Marshal(struct {
			Panel  reports.Panel
			Status []string
		}{identity, p.Query.StatusKeys})
		// StaticData is intentionally not in Panel's JSON; authored panels stay unique.
		key := fmt.Sprintf("%x", sha256.Sum256(raw))
		if !reports.IsLive(p.Type) && p.Source == nil {
			key = fmt.Sprintf("static:%d", i)
		}
		if index, ok := byKey[key]; ok {
			jobs[index].indices = append(jobs[index].indices, i)
		} else {
			byKey[key] = len(jobs)
			jobs = append(jobs, job{p, []int{i}, key})
		}
	}
	queue := make(chan job)
	var workers sync.WaitGroup
	for n := 0; n < min(reportWorkers, len(jobs)); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			req := r
			closeRead := func() {}
			token := ""
			var readErr error
			if store, canonical := opts.primitiveStore.(*primitives.Store); canonical {
				ctx, close, err := store.BeginReportRead(r.Context())
				readErr = err
				if err == nil {
					req = r.WithContext(ctx)
					closeRead = close
					token, _ = store.ReportRenderToken(ctx)
				}
			}
			defer closeRead()
			for j := range queue {
				cacheable := reports.IsLive(j.panel.Type) && j.panel.Source == nil && j.panel.Type != "live-fleet-health" && !(j.panel.Type == "live-collection" && j.panel.Query.Collection == "agents")
				jobToken := ""
				if cacheable {
					jobToken = token
				}

				key := jobToken + "|" + r.URL.Path + "|" + r.URL.Query().Get("summary") + "|" + r.Header.Get("X-ANX-Report-Revision") + "|" + j.key
				var result map[string]any
				if jobToken != "" {
					result = opts.reportPanels.get(key, now)
				}
				if result == nil {
					if readErr != nil {
						result = map[string]any{"status": "unavailable", "data": map[string]any{}, "message": "Report data is unavailable.", "truncated": false, "observed_at": now.Format(time.RFC3339Nano), "provenance_class": "live"}
					} else {
						reader := reportReader{r: req, opts: opts, now: now, visibility: map[string]bool{}, decisionLimit: min(maxReportCandidates, j.panel.Query.Limit+1)}
						result = reader.renderPanel(j.panel)
						if jobToken != "" {
							opts.reportPanels.put(key, result, now)
						}
					}
				}
				for _, i := range j.indices {
					copy := map[string]any{}
					for k, v := range result {
						copy[k] = v
					}
					copy["id"], copy["type"], copy["role"] = panels[i].ID, panels[i].Type, reports.PanelRole(panels[i].Type, panels[i].Role)
					results[i] = copy
				}
			}
		}()
	}
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	workers.Wait()
	return now.Format(time.RFC3339Nano), results
}

func (reader *reportReader) renderPanel(panel reports.Panel) map[string]any {
	now := reader.now
	if !reports.IsLive(panel.Type) && panel.Source == nil {
		due, _ := time.Parse(time.RFC3339Nano, panel.ReviewBy)
		return map[string]any{"status": "ok", "observed_at": panel.AuthoredAt, "truncated": false, "data": panel.StaticData, "provenance_class": "authored", "author": panel.Author, "authored_at": panel.AuthoredAt, "review_by": panel.ReviewBy, "review_by_defaulted": panel.ReviewByDefaulted, "review_due": !now.Before(due)}
	}
	if panel.Source != nil {
		result := reader.materializeSeries(panel)
		result["provenance_class"] = "live"
		return result
	}
	data, truncated, err := reader.materialize(panel)
	result := map[string]any{"status": "ok", "observed_at": now.Format(time.RFC3339Nano), "truncated": truncated, "data": data, "provenance_class": "live"}
	if err != nil {
		result["status"] = "unavailable"
		result["data"] = map[string]any{}
		result["message"] = "This panel could not be read with your current access. Try again or check the source."
	}
	return result
}
