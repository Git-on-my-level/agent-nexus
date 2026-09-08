package observation

import (
	"context"
	"encoding/json"
	"fmt"
)

type MulticaReader struct{ source *httpSource }

func NewMulticaReader(c HTTPConfig) (*MulticaReader, error) {
	if c.SourceWorkspaceID == "" {
		return nil, failure(ErrConfiguration, "Multica requires an approved source workspace ID")
	}
	s, err := newHTTPSource(c)
	if err != nil {
		return nil, err
	}
	return &MulticaReader{source: s}, nil
}
func (*MulticaReader) Capabilities() Capabilities {
	return Capabilities{Source: "multica", ReadOne: true, TrustedBuiltin: true, EvidenceKinds: []string{"issue", "task_run", "pull_request"}, Limitations: []string{"Selected issues only; task-runs endpoint is unpaginated and output is bounded; run success is not work acceptance"}}
}
func (r *MulticaReader) Read(ctx context.Context, t Target) (Report, error) {
	if err := r.source.bind(t, "multica"); err != nil {
		return Report{}, err
	}
	if t.Kind != "issue" || !validComponent(t.NativeID) {
		return Report{}, failure(ErrConfiguration, "Multica requires selected issue ID or identifier")
	}
	ctx, cancel := context.WithTimeout(ctx, r.source.config.Timeout)
	defer cancel()
	s, err := r.source.session(ctx)
	if err != nil {
		return Report{}, err
	}
	var issue map[string]any
	_, err = s.get(ctx, "/api/issues/"+t.NativeID, &issue)
	if err != nil {
		return Report{}, err
	}
	id := str(issue, "id")
	if !validComponent(id) || (t.NativeID != id && t.NativeID != str(issue, "identifier")) || str(issue, "workspace_id") != r.source.config.SourceWorkspaceID {
		return Report{}, failure(ErrPermission, "Multica response differs from approved issue or workspace")
	}
	if str(issue, "title") == "" || str(issue, "status") == "" {
		return Report{}, failure(ErrInvalidOutput, "Multica issue required fields missing")
	}
	out := newReport(t, "builtin:multica")
	out.Title = str(issue, "title")
	out.NativeStatus = str(issue, "status")
	out.URL = r.source.base.String() + "/api/issues/" + id
	out.SourceUpdatedAt = stamp(str(issue, "updated_at"))
	out.SourceActivityAt = stamp(str(issue, "last_activity_at"))
	out.SourceRevision = str(issue, "updated_at")
	if rev, ok := issue["revision"].(float64); ok {
		out.SourceRevision = fmt.Sprintf("%.0f", rev)
	}
	out.Facts = selectFields(issue, "title", "identifier", "priority", "assignee_id", "assignee_type", "parent_issue_id", "project_id", "start_date", "due_date", "status_category")
	out.Facts["native_status"] = out.NativeStatus
	out.Facts["phase"] = phase(out.NativeStatus)
	if category := str(issue, "status_category"); category != "" {
		out.Facts["phase"] = phase(category)
	}
	if owner := str(issue, "assignee_id"); owner != "" {
		out.Facts["owner"] = owner
	}
	out.Evidence = append(out.Evidence, Evidence{Kind: "issue", Reference: out.URL, Revision: out.SourceRevision, Knowledge: "reported"})
	var runs []map[string]any
	_, err = s.get(ctx, "/api/issues/"+id+"/task-runs", &runs)
	if err != nil {
		partial(&out, err)
	} else {
		if len(runs) > 100 {
			runs = runs[:100]
			partial(&out, failure(ErrLimit, "task-run output capped at 100; source endpoint has no pagination"))
		}
		selected := []map[string]any{}
		for _, run := range runs {
			if foreign := str(run, "issue_id"); foreign != "" && foreign != id {
				partial(&out, failure(ErrInvalidOutput, "unrelated run omitted"))
				continue
			}
			rid := str(run, "id")
			if !validComponent(rid) {
				partial(&out, failure(ErrInvalidOutput, "invalid run identity omitted"))
				continue
			}
			selected = append(selected, selectFields(run, "id", "status", "agent_id", "model", "started_at", "completed_at", "parent_run_id", "continuation_of_run_id"))
			out.Evidence = append(out.Evidence, Evidence{Kind: "task_run", Reference: out.URL + "/task-runs#" + rid, Revision: rid, Knowledge: "reported", Summary: str(run, "status")})
		}
		out.Facts["runs"] = selected
	}
	// Explicit source PR links are evidence; never join tasks by title.
	if s.pages < s.source.config.MaxPages {
		var raw json.RawMessage
		_, err = s.get(ctx, "/api/issues/"+id+"/pull-requests", &raw)
		if err != nil {
			partial(&out, err)
		} else {
			var links []map[string]any
			if len(raw) > 0 && raw[0] == '[' {
				err = json.Unmarshal(raw, &links)
			} else {
				var obj struct {
					PullRequests []map[string]any `json:"pull_requests"`
				}
				err = json.Unmarshal(raw, &obj)
				links = obj.PullRequests
			}
			if err != nil {
				partial(&out, failure(ErrInvalidOutput, "invalid linked PR collection"))
			} else {
				if len(links) > 100 {
					links = links[:100]
					partial(&out, failure(ErrLimit, "linked PR output capped at 100"))
				}
				for _, link := range links {
					ref := str(link, "url")
					if ref == "" {
						ref = str(link, "html_url")
					}
					if safeReference(ref) {
						out.Evidence = append(out.Evidence, Evidence{Kind: "pull_request", Reference: ref, Knowledge: "reported"})
					}
				}
			}
		}
	} else {
		partial(&out, failure(ErrLimit, "linked PR evidence not read: request budget exhausted"))
	}
	out.Coverage.Pages = s.pages
	return finishReport(out)
}
