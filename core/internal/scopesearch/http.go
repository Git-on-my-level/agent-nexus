package scopesearch

import (
	"net/http"
)

// HTTPOptions is dispatcher setup. A owns route/contract registration and
// supplies authenticated selection; request text never chooses a principal.
// Nil Ready keeps this path disabled, including in all current server defaults.
type HTTPOptions struct {
	Repository Repository
	Tokens     *Tokens
	Ready      func() bool
	Selection  func(*http.Request) (string, []string, error)
}
