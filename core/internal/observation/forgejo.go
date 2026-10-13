package observation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
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

// origin is the registered forge origin (lowercase host:port, HTTPS default
// 443). Returned identity URLs must point here.
func (r *ForgejoReader) origin() string {
	port := r.source.base.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(r.source.base.Hostname()) + ":" + port
}

// forgeIdentityURL reports whether a returned URL is this exact resource on
// the registered origin: {owner}/{repo}/{issues|pulls}/{nativeID} (marker
// restricted). Fragments are ignored; the path number must be the same
// canonical decimal as the requested native ID.
func forgeIdentityURL(raw string, origin, repository, nativeID string, markers ...string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	if strings.ToLower(u.Hostname())+":"+port != origin {
		return nil, false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 4 || segments[0]+"/"+segments[1] != repository || segments[3] != nativeID {
		return nil, false
	}
	for _, marker := range markers {
		if segments[2] == marker {
			return u, true
		}
	}
	return nil, false
}

// forgeNumber decodes a JSON number exactly. Nearby integers must never
// compare equal to the requested identity through float64 rounding.
func forgeNumber(v any) (int64, bool) {
	number, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	value, err := number.Int64()
	return value, err == nil
}

func (r *ForgejoReader) Read(ctx context.Context, t Target) (Report, error) {
	if err := r.source.bind(t, r.sourceName); err != nil {
		return Report{}, err
	}
	n, err := strconv.ParseUint(t.NativeID, 10, 63)
	if err != nil || n == 0 || strconv.FormatUint(n, 10) != t.NativeID || !validRepo(t.Repository) || (t.Kind != "issue" && t.Kind != "pull_request") {
		return Report{}, failure(ErrConfiguration, "Forgejo target requires owner/repository, numeric index, and issue or pull_request kind")
	}
	ctx, cancel := context.WithTimeout(ctx, r.source.config.Timeout)
	defer cancel()
	session, err := r.source.session(ctx)
	if err != nil {
		return Report{}, err
	}
	origin := r.origin()
	endpoint := r.api("/repos/" + t.Repository + "/issues/" + t.NativeID)
	// Decode the identity payload with exact numbers: an issue number is an
	// identifier, and float64 rounding could accept a nearby but wrong one.
	var raw json.RawMessage
	_, err = session.get(ctx, endpoint, &raw)
	if err != nil {
		return Report{}, err
	}
	issue, err := decodeJSONNumbers(raw)
	if err != nil {
		return Report{}, failure(ErrInvalidOutput, "forge returned invalid JSON")
	}
	if value, ok := forgeNumber(issue["number"]); !ok || value != int64(n) || str(issue, "title") == "" || str(issue, "state") == "" {
		return Report{}, failure(ErrInvalidOutput, "forge issue identity or required fields missing")
	}
	out := newReport(t, "builtin:"+r.sourceName)
	out.Title = str(issue, "title")
	out.NativeStatus = str(issue, "state")
	// The returned identity URL is evidence: it must name the registered
	// origin, this repository and this issue number.
	htmlURL := str(issue, "html_url")
	if _, ok := forgeIdentityURL(htmlURL, origin, t.Repository, t.NativeID, "issues", "pulls"); !ok {
		return Report{}, failure(ErrInvalidOutput, "forge issue URL does not identify the registered resource")
	}
	out.URL = htmlURL
	out.SourceUpdatedAt = stamp(str(issue, "updated_at"))
	out.SourceActivityAt = out.SourceUpdatedAt
	out.SourceRevision = str(issue, "updated_at")
	out.Facts = selectFields(issue, "title", "locked", "comments", "created_at", "closed_at")
	out.Facts["native_status"] = out.NativeStatus
	out.Facts["phase"] = phase(out.NativeStatus)
	if assignee, ok := issue["assignee"].(map[string]any); ok {
		out.Facts["owner"] = str(assignee, "login")
	}
	out.Evidence = append(out.Evidence, Evidence{Kind: "issue", Reference: htmlURL, Revision: out.SourceRevision, Knowledge: "reported"})
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
			prURL := str(pr, "html_url")
			if _, ok := forgeIdentityURL(prURL, origin, t.Repository, t.NativeID, "pulls"); !ok {
				return Report{}, failure(ErrInvalidOutput, "forge pull-request URL does not identify the registered resource")
			}
			out.Evidence = append(out.Evidence, Evidence{Kind: "pull_request", Reference: prURL, Revision: out.SourceRevision, Knowledge: "reported"})
			reviews, e := session.list(ctx, r.api("/repos/"+t.Repository+"/pulls/"+t.NativeID+"/reviews"), "", &out)
			if e != nil {
				partial(&out, e)
			}
			considered := 0
			dropped := 0
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
				// A review URL off this pull request is not evidence of it.
				if _, ok := forgeIdentityURL(reference, origin, t.Repository, t.NativeID, "pulls"); !ok {
					dropped++
					continue
				}
				out.Evidence = append(out.Evidence, Evidence{Kind: "review", Reference: reference, Revision: str(review, "commit_id"), Knowledge: "reported", Summary: str(review, "state")})
			}
			if dropped > 0 {
				out.Coverage.Limitations = append(out.Coverage.Limitations, "review references outside this pull request omitted")
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
						out.Facts["status_check"] = selectFields(combined, "state", "total_count")
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
							// A status target is a deep link, often to external CI;
							// only its URL sanity is checked, not its origin.
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
	} else if count, ok := forgeNumber(issue["comments"]); ok && count > 0 {
		comments, e := session.list(ctx, endpoint+"/comments", "", &out)
		if e != nil {
			partial(&out, e)
		}
		dropped := 0
		for _, comment := range comments {
			ref := str(comment, "html_url")
			// A comment URL off this issue is not evidence of it.
			if _, ok := forgeIdentityURL(ref, origin, t.Repository, t.NativeID, "issues", "pulls"); !ok {
				dropped++
				continue
			}
			out.Evidence = append(out.Evidence, Evidence{Kind: "comment", Reference: ref, Revision: str(comment, "updated_at"), Knowledge: "reported"})
		}
		if dropped > 0 {
			out.Coverage.Limitations = append(out.Coverage.Limitations, "comment references outside this issue omitted")
		}
	}
	out.Coverage.Pages = session.pages
	return finishReport(out)
}

// decodeJSONNumbers decodes one JSON object keeping numbers exact.
func decodeJSONNumbers(raw json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
