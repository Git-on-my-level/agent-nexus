package observation

import (
	"context"
	"strconv"
)

// ForgejoReader reads issue and pull-request state, reviews, commit statuses and
// comments from a self-hosted Forgejo or Gitea forge over its Gitea-compatible
// v1 HTTP API. It is read-only and bound to one approved connection.
type ForgejoReader struct {
	source     *httpSource
	sourceName string
}

// NewForgejoReader requires the forge web origin (the API is mounted under
// /api/v1) because self-hosted forges have no well-known base URL.
func NewForgejoReader(c HTTPConfig, source string) (*ForgejoReader, error) {
	if source != "forgejo" && source != "gitea" {
		return nil, failure(ErrConfiguration, "forge reader requires the forgejo or gitea source kind")
	}
	if c.BaseURL == "" {
		return nil, failure(ErrConfiguration, "forgejo requires the forge HTTPS origin as base URL")
	}
	c.TokenScheme = "token"
	s, err := newHTTPSource(c)
	if err != nil {
		return nil, err
	}
	return &ForgejoReader{source: s, sourceName: source}, nil
}

func (r *ForgejoReader) Capabilities() Capabilities {
	return Capabilities{Source: r.sourceName, ReadOne: true, TrustedBuiltin: true, EvidenceKinds: []string{"issue", "pull_request", "comment", "review", "check_run"}, Limitations: []string{"Selected targets only; no deployment or product acceptance verification; no artifact downloads"}}
}

// api prefixes built-in endpoints with the Gitea-compatible v1 mount so the
// operator configures the forge origin rather than an API path.
func (r *ForgejoReader) api(path string) string { return "/api/v1" + path }

func (r *ForgejoReader) Read(ctx context.Context, t Target) (Report, error) {
	if err := r.source.bind(t, r.sourceName); err != nil {
		return Report{}, err
	}
	n, err := strconv.ParseUint(t.NativeID, 10, 64)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != t.NativeID || !validRepo(t.Repository) || (t.Kind != "issue" && t.Kind != "pull_request") {
		return Report{}, failure(ErrConfiguration, "Forgejo target requires owner/repository, numeric index, and issue or pull_request kind")
	}
	ctx, cancel := context.WithTimeout(ctx, r.source.config.Timeout)
	defer cancel()
	session, err := r.source.session(ctx)
	if err != nil {
		return Report{}, err
	}
	endpoint := r.api("/repos/" + t.Repository + "/issues/" + t.NativeID)
	var issue map[string]any
	_, err = session.get(ctx, endpoint, &issue)
	if err != nil {
		return Report{}, err
	}
	if num, ok := issue["number"].(float64); !ok || num != float64(n) || str(issue, "title") == "" || str(issue, "state") == "" {
		return Report{}, failure(ErrInvalidOutput, "forge issue identity or required fields missing")
	}
	out := newReport(t, "builtin:"+r.sourceName)
	out.Title = str(issue, "title")
	out.NativeStatus = str(issue, "state")
	out.URL = str(issue, "html_url")
	out.SourceUpdatedAt = stamp(str(issue, "updated_at"))
	out.SourceActivityAt = out.SourceUpdatedAt
	out.SourceRevision = str(issue, "updated_at")
	out.Facts = selectFields(issue, "title", "locked", "comments", "created_at", "closed_at")
	out.Facts["native_status"] = out.NativeStatus
	out.Facts["phase"] = phase(out.NativeStatus)
	if assignee, ok := issue["assignee"].(map[string]any); ok {
		out.Facts["owner"] = str(assignee, "login")
	}
	if reference := str(issue, "html_url"); reference != "" {
		out.Evidence = append(out.Evidence, Evidence{Kind: "issue", Reference: reference, Revision: out.SourceRevision, Knowledge: "reported"})
	}
	_, isPR := issue["pull_request"]
	if t.Kind == "pull_request" && !isPR {
		return Report{}, failure(ErrInvalidOutput, "selected target is not a pull request")
	}
	if isPR {
		var pr map[string]any
		_, err = session.get(ctx, r.api("/repos/"+t.Repository+"/pulls/"+t.NativeID), &pr)
		if err != nil {
			partial(&out, err)
		} else {
			out.Facts["pull_request"] = selectFields(pr, "merged", "merged_at", "merge_commit_sha", "draft", "mergeable")
			if merged, ok := pr["merged"].(bool); ok && merged {
				out.NativeStatus = "merged"
				out.Facts["native_status"] = "merged"
				out.Facts["phase"] = "done"
			}
			// The pull-request URL is the identity other surfaces reference; keep
			// it as a lookup key even when the issue html_url already matches.
			if reference := str(pr, "html_url"); reference != "" {
				out.Evidence = append(out.Evidence, Evidence{Kind: "pull_request", Reference: reference, Revision: out.SourceRevision, Knowledge: "reported"})
			}
			reviews, e := session.list(ctx, r.api("/repos/"+t.Repository+"/pulls/"+t.NativeID+"/reviews"), "", &out)
			if e != nil {
				partial(&out, e)
			}
			considered := 0
			for _, review := range reviews {
				considered++
				if considered > 100 {
					out.Coverage.Complete = false
					out.Coverage.Limitations = append(out.Coverage.Limitations, "review list truncated at 100")
					break
				}
				reference := str(review, "html_url")
				if reference == "" {
					reference = str(review, "pull_request_url")
				}
				if reference != "" {
					out.Evidence = append(out.Evidence, Evidence{Kind: "review", Reference: reference, Revision: str(review, "commit_id"), Knowledge: "reported", Summary: str(review, "state")})
				}
			}
			if head, ok := pr["head"].(map[string]any); ok {
				sha := str(head, "sha")
				if validSHA(sha) {
					out.Facts["head_sha"] = sha
					var combined map[string]any
					_, e = session.get(ctx, r.api("/repos/"+t.Repository+"/commits/"+sha+"/status"), &combined)
					if e != nil {
						partial(&out, e)
					} else {
						out.Facts["status_check"] = selectFields(combined, "status", "total_count")
						statuses, _ := combined["statuses"].([]any)
						for i, raw := range statuses {
							if i >= 20 {
								out.Coverage.Complete = false
								out.Coverage.Limitations = append(out.Coverage.Limitations, "commit status list truncated at 20")
								break
							}
							status, ok := raw.(map[string]any)
							if !ok {
								continue
							}
							reference := str(status, "target_url")
							if reference == "" {
								continue
							}
							out.Evidence = append(out.Evidence, Evidence{Kind: "check_run", Reference: reference, Revision: sha, Knowledge: "reported", Summary: str(status, "context") + ": " + str(status, "status")})
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
