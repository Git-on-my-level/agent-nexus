package scopesearch

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestSearchTokenBoundCoversAcceptedEscapedKeys(t *testing.T) {
	tokens, err := NewTokens([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, character := range []string{"<", ">", "&", "\x01"} {
		t.Run(character, func(t *testing.T) {
			cursor := searchCursor{Binding: strings.Repeat("b", 64), Query: strings.Repeat("a", 64), After: &Key{Recency: math.MaxInt64, RID: strings.Repeat(character, maxResourceIDBytes), Kind: "document", Scope: strings.Repeat(character, maxScopeIDBytes)}}
			plain, err := json.Marshal(cursor)
			if err != nil {
				t.Fatal(err)
			}
			if maxCursorJSONBytes != 4825 {
				t.Fatalf("documented cursor bound drift: %d", maxCursorJSONBytes)
			}
			if len(plain) != maxCursorJSONBytes {
				t.Fatalf("cursor bound drift: actual=%d bound=%d", len(plain), maxCursorJSONBytes)
			}
			token, err := tokens.seal(cursor)
			if err != nil {
				t.Fatal(err)
			}
			if len(token) != tokens.maxEncodedBytes() || len(token) != 6471 {
				t.Fatalf("wire bound drift: actual=%d bound=%d", len(token), tokens.maxEncodedBytes())
			}
			var resumed searchCursor
			if err := tokens.open(token, &resumed); err != nil || !reflect.DeepEqual(cursor, resumed) {
				t.Fatalf("accepted cursor did not round trip: %v", err)
			}
			if err := tokens.open(token+"a", &resumed); !errors.Is(err, ErrCursor) {
				t.Fatal("oversized token accepted", err)
			}
			cursor.After.RID += character
			if token, err := tokens.seal(cursor); !errors.Is(err, ErrCursor) || token != "" {
				t.Fatal("seal minted token outside derived bound", err)
			}
		})
	}
}

func TestSearchTokenBoundRetainsAuthentication(t *testing.T) {
	tokens, err := NewTokens([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.seal(searchCursor{Binding: strings.Repeat("b", 64), Query: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 1
	var resumed searchCursor
	if err := tokens.open(base64.RawURLEncoding.EncodeToString(b), &resumed); !errors.Is(err, ErrCursor) {
		t.Fatal("tampered cursor accepted", err)
	}
}
