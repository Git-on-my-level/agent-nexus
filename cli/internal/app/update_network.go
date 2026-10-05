package app

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var updateHTTPTransport http.RoundTripper = http.DefaultTransport
var releaseAssetHosts = map[string]bool{
	"release-assets.githubusercontent.com":  true,
	"objects.githubusercontent.com":         true,
	"github-releases.githubusercontent.com": true,
}

// All entry URLs are compiled-in repository endpoints. Test fixtures replace
// those endpoints and the transport, never the HTTPS or redirect policy.
func updateHTTPClient(timeout time.Duration, kind, entry string) (*http.Client, error) {
	origin, err := url.Parse(entry)
	if err != nil {
		return nil, err
	}
	if origin.Scheme != "https" || origin.User != nil {
		return nil, fmt.Errorf("release endpoint must use HTTPS")
	}
	client := &http.Client{Timeout: timeout, Transport: updateHTTPTransport}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		u := req.URL
		if len(via) >= 5 || u.Scheme != "https" || u.User != nil {
			return fmt.Errorf("unsafe release redirect")
		}
		switch kind {
		case "api":
			// The API has a canonical repository path; do not follow repository moves.
			return fmt.Errorf("release API redirects are not accepted")
		case "latest":
			base := strings.TrimSuffix(origin.Path, "latest")
			if u.Host != origin.Host || !strings.HasPrefix(u.Path, base+"tag/") || strings.Contains(strings.TrimPrefix(u.Path, base+"tag/"), "/") {
				return fmt.Errorf("release discovery escaped repository")
			}
		case "asset":
			if u.Host == origin.Host {
				if u.Path != origin.Path {
					return fmt.Errorf("release asset escaped repository or filename")
				}
			} else if !releaseAssetHosts[u.Hostname()] || u.Port() != "" && u.Port() != "443" {
				return fmt.Errorf("unapproved release asset host")
			}
		default:
			return fmt.Errorf("unknown release request policy")
		}
		return nil
	}
	return client, nil
}
