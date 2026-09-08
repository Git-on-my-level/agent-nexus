package observation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type GitHubReader struct{ source *httpSource }

func NewGitHubReader(c HTTPConfig) (*GitHubReader, error) {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.github.com"
	}
	s, err := newHTTPSource(c)
	if err != nil {
		return nil, err
	}
	return &GitHubReader{source: s}, nil
}
func (*GitHubReader) Capabilities() Capabilities {
	return Capabilities{Source: "github", ReadOne: true, TrustedBuiltin: true, EvidenceKinds: []string{"issue", "pull_request", "comment", "review", "check_run"}, Limitations: []string{"Selected targets only; no deployment or product acceptance verification; no artifact downloads"}}
}
func (r *GitHubReader) Read(ctx context.Context, t Target) (Report, error) {
	if err := r.source.bind(t, "github"); err != nil {
		return Report{}, err
	}
	n, err := strconv.ParseUint(t.NativeID, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != t.NativeID || !validRepo(t.Repository) || (t.Kind != "issue" && t.Kind != "pull_request") {
		return Report{}, failure(ErrConfiguration, "GitHub target requires owner/repository, numeric ID, and issue or pull_request kind")
	}
	ctx, cancel := context.WithTimeout(ctx, r.source.config.Timeout)
	defer cancel()
	session, err := r.source.session(ctx)
	if err != nil {
		return Report{}, err
	}
	endpoint := "/repos/" + t.Repository + "/issues/" + t.NativeID
	var issue map[string]any
	_, err = session.get(ctx, endpoint, &issue)
	if err != nil {
		return Report{}, err
	}
	if num, ok := issue["number"].(float64); !ok || num != float64(n) || str(issue, "title") == "" || str(issue, "state") == "" {
		return Report{}, failure(ErrInvalidOutput, "GitHub issue identity or required fields missing")
	}
	out := newReport(t, "builtin:github")
	out.Title = str(issue, "title")
	out.NativeStatus = str(issue, "state")
	out.URL = str(issue, "html_url")
	out.SourceUpdatedAt = stamp(str(issue, "updated_at"))
	out.SourceActivityAt = out.SourceUpdatedAt
	out.SourceRevision = str(issue, "updated_at")
	out.Facts = selectFields(issue, "title", "state_reason", "locked", "comments", "created_at", "closed_at")
	out.Facts["native_status"] = out.NativeStatus
	out.Facts["phase"] = phase(out.NativeStatus)
	if assignee, ok := issue["assignee"].(map[string]any); ok {
		out.Facts["owner"] = str(assignee, "login")
	}
	out.Evidence = append(out.Evidence, Evidence{Kind: "issue", Reference: out.URL, Revision: out.SourceRevision, Knowledge: "reported"})
	_, isPR := issue["pull_request"]
	if t.Kind == "pull_request" && !isPR {
		return Report{}, failure(ErrInvalidOutput, "selected target is not a pull request")
	}
	if isPR {
		var pr map[string]any
		_, err = session.get(ctx, "/repos/"+t.Repository+"/pulls/"+t.NativeID, &pr)
		if err != nil {
			partial(&out, err)
		} else {
			out.Facts["pull_request"] = selectFields(pr, "merged", "merged_at", "merge_commit_sha", "draft", "mergeable", "mergeable_state")
			if merged, ok := pr["merged"].(bool); ok && merged {
				out.NativeStatus = "merged"
				out.Facts["native_status"] = "merged"
				out.Facts["phase"] = "done"
			}
			reviews, e := session.list(ctx, "/repos/"+t.Repository+"/pulls/"+t.NativeID+"/reviews", "", &out)
			if e != nil {
				partial(&out, e)
			}
			for _, review := range reviews {
				ref := str(review, "html_url")
				if ref != "" {
					out.Evidence = append(out.Evidence, Evidence{Kind: "review", Reference: ref, Revision: str(review, "commit_id"), Knowledge: "reported", Summary: str(review, "state")})
				}
			}
			if head, ok := pr["head"].(map[string]any); ok {
				sha := str(head, "sha")
				if validSHA(sha) {
					out.Facts["head_sha"] = sha
					checks, e := session.list(ctx, "/repos/"+t.Repository+"/commits/"+sha+"/check-runs", "check_runs", &out)
					if e != nil {
						partial(&out, e)
					}
					for _, check := range checks {
						ref := str(check, "html_url")
						if ref != "" {
							out.Evidence = append(out.Evidence, Evidence{Kind: "check_run", Reference: ref, Revision: sha, Knowledge: "reported", Summary: str(check, "name") + ": " + str(check, "status") + " / " + str(check, "conclusion")})
						}
					}
				}
			}
		}
	} else if count, ok := issue["comments"].(float64); ok && count > 0 {
		comments, e := session.list(ctx, endpoint+"/comments", "", &out)
		if e != nil {
			partial(&out, e)
		}
		for _, comment := range comments {
			ref := str(comment, "html_url")
			if ref != "" {
				out.Evidence = append(out.Evidence, Evidence{Kind: "comment", Reference: ref, Revision: str(comment, "updated_at"), Knowledge: "reported"})
			}
		}
	}
	out.Coverage.Pages = session.pages
	return finishReport(out)
}
func validSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s *readSession) list(ctx context.Context, endpoint, arrayKey string, out *Report) ([]map[string]any, error) {
	var result []map[string]any
	for page := 1; ; page++ {
		if s.pages >= s.source.config.MaxPages {
			out.Coverage.NextCursor = fmt.Sprintf("%s?page=%d", endpoint, page)
			out.Coverage.Complete = false
			return result, failure(ErrLimit, "source pagination budget reached")
		}
		var raw json.RawMessage
		h, err := s.get(ctx, fmt.Sprintf("%s?per_page=100&page=%d", endpoint, page), &raw)
		if err != nil {
			return result, err
		}
		if arrayKey != "" {
			var obj map[string]json.RawMessage
			if json.Unmarshal(raw, &obj) != nil || obj[arrayKey] == nil {
				return result, failure(ErrInvalidOutput, "missing source collection")
			}
			raw = obj[arrayKey]
		}
		var items []map[string]any
		if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &items) != nil {
			return result, failure(ErrInvalidOutput, "invalid source collection")
		}
		if len(items) > 100 {
			items = items[:100]
			out.Coverage.Complete = false
			out.Coverage.Limitations = append(out.Coverage.Limitations, "source returned more than requested page size")
		}
		result = append(result, items...)
		if !hasNext(h) {
			return result, nil
		}
	}
}
func hasNext(h http.Header) bool {
	for _, link := range strings.Split(h.Get("Link"), ",") {
		if strings.Contains(link, `rel="next"`) || strings.Contains(link, "rel=next") {
			return true
		}
	}
	return false
}
