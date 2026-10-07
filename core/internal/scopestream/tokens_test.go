package scopestream

import (
	"errors"
	"strings"
	"testing"
)

func TestTokenSealOpenAtEncodedSizeBoundary(t *testing.T) {
	tokens := testTokens(t)
	maxJSON := MaxTokenBytes*3/4 - tokens.aead.NonceSize() - tokens.aead.Overhead()
	value := strings.Repeat("x", maxJSON-2) // String's two JSON quotes count too.
	token, err := tokens.seal(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) > MaxTokenBytes {
		t.Fatal("minted unresumable token")
	}
	var decoded string
	if err = tokens.open(token, &decoded); err != nil || decoded != value {
		t.Fatal("maximum token does not round-trip")
	}
	if _, err = tokens.seal(value + "x"); !errors.Is(err, ErrBudget) {
		t.Fatalf("oversized encoded token accepted: %v", err)
	}
}
