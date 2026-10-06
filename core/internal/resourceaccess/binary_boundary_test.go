package resourceaccess

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
)

func TestReferenceBoundaryEveryByte(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const target = "document:[]"
	for n := 0; n <= 255; n++ {
		b := byte(n)
		// Single non-ASCII bytes are malformed UTF-8, hence boundaries. Valid
		// multi-byte identifier runes and controls are tested separately below.
		want := !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_')
		for _, side := range []string{"before", "after"} {
			t.Run(fmt.Sprintf("%02x/%s", b, side), func(t *testing.T) {
				input := string([]byte{b}) + target
				if side == "after" {
					input = target + string([]byte{b})
				}
				atom := textReferencePrefix + input
				if got := textHasReference(atom, target); got != want {
					t.Fatalf("Go boundary for %q=%v want %v", input, got, want)
				}
				var got bool
				if err := db.QueryRow(`SELECT `+TextReferenceMatchSQL("?", "?"), atom, atom, target).Scan(&got); err != nil || got != want {
					t.Fatalf("SQL boundary=%v want %v err=%v", got, want, err)
				}
				for _, media := range []string{"binary", "image/png", "text", "text/plain", "structured", "application/json", "application/example+json"} {
					body := input
					if media == "structured" || media == "application/json" || media == "application/example+json" {
						encoded, _ := json.Marshal(map[string]any{"text": input})
						body = string(encoded)
					}
					var atoms []string
					if err := json.Unmarshal([]byte(ContentReferenceAtomsJSON(body, media)), &atoms); err != nil {
						t.Fatal(err)
					}
					matched := false
					for _, candidate := range atoms {
						matched = matched || candidate == target || textHasReference(candidate, target)
					}
					if matched != want {
						t.Fatalf("%s manifest match=%v want %v: %q", media, matched, want, atoms)
					}
				}
			})
		}
	}
}

func TestReferenceBoundaryUnicodeControlsAndIdentifiers(t *testing.T) {
	for r := rune(0x80); r <= 0x9f; r++ {
		for _, input := range []string{string(r) + "document:[]", "document:[]" + string(r)} {
			if !textHasReference(textReferencePrefix+input, "document:[]") {
				t.Errorf("C1 control U+%04X hid ref in %q", r, input)
			}
		}
	}
	for _, r := range []rune{'é', '界', '９', '\u0301', '-', '_'} {
		for _, input := range []string{string(r) + "document:[]", "document:[]" + string(r)} {
			if textHasReference(textReferencePrefix+input, "document:[]") {
				t.Errorf("identifier continuation U+%04X matched in %q", r, input)
			}
		}
	}
}

func TestBinaryContentValidationDoesNotInterpretJSON(t *testing.T) {
	for _, body := range [][]byte{[]byte(`{"text":"\u0000"}`), []byte(`"\u0000"`), []byte("prose\x00document:[]\x00")} {
		for _, media := range []string{"binary", "image/png", "application/octet-stream", "application/pdf"} {
			if err := ValidateContent(body, media); err != nil {
				t.Fatalf("%s rejected binary %q: %v", media, body, err)
			}
		}
		for _, media := range []string{"text", "structured", "text/plain", "application/json", "application/example+json"} {
			if err := ValidateContent(body, media); err != ErrNULText {
				t.Fatalf("%s accepted NUL text %q: %v", media, body, err)
			}
		}
	}
}
