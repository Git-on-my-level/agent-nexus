package reject

import (
	"crypto/rand"
	. "crypto/rand"
)

// Mutate is the fixture foreignvar must reject. Assignment and address-of
// rebind crypto/rand.Reader, including through a dot import.
func Mutate() {
	rand.Reader = nil
	_ = &rand.Reader
	Reader = nil
	_ = &Reader
}
